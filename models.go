package main

import (
	"database/sql"
	"errors"
	"net/http"
	"time"

	"github.com/aykevl/south"
)

type Hint int

const (
	FETCH_TITLE = iota + 1
	FETCH_ALL
)

type PageType int

const (
	PAGE_TYPE_NONE = iota
	PAGE_TYPE_POST
	PAGE_TYPE_STATIC
)

var PageTypeNames = map[PageType]string{
	PAGE_TYPE_POST:   "post",
	PAGE_TYPE_STATIC: "page",
}

type Page struct {
	Id        int64
	Name      string
	Title     string
	Type      PageType
	Summary   string
	Created   time.Time
	Published time.Time
	Modified  time.Time
	Text      string
}

func NewPage(pageType PageType) *Page {
	now := time.Now()
	return &Page{Type: pageType, Created: now, Modified: now}
}

func PageFromQuery(blog *Blog, pageType PageType, hint Hint, whereClause, otherClauses string, args ...interface{}) *Page {
	pages := PagesFromQuery(blog, pageType, hint, whereClause, otherClauses, args...)
	if len(pages) > 1 {
		raiseError("tried to fetch one page, but got more than one")
	}

	if len(pages) == 0 {
		return nil
	}

	return pages[0]
}

type Pages []*Page

func PagesFromQuery(blog *Blog, pageType PageType, hint Hint, whereClause, otherClauses string, args ...interface{}) Pages {
	var pages []*Page

	query := "SELECT id, name, title, type, summary, published, modified FROM pages "
	if hint == FETCH_ALL {
		query = "SELECT id, name, title, type, summary, created, published, modified, text FROM pages "
	}

	if pageType == PAGE_TYPE_NONE {
		if len(whereClause) > 0 {
			query += "WHERE " + whereClause + " "
		}

	} else {
		query += "WHERE type=? "

		args = append([]interface{}{pageType}, args...)

		if len(whereClause) > 0 {
			query += "AND (" + whereClause + ") "
		}
	}

	query += otherClauses

	rows, err := blog.db.Query(query, args...)
	checkError(err, "failed to fetch list of pages")
	defer rows.Close()

	for rows.Next() {
		page := &Page{}
		var publishedUnix, modifiedUnix int64

		if hint == FETCH_TITLE {
			err = rows.Scan(&page.Id, &page.Name, &page.Title, &page.Type, &page.Summary, &publishedUnix, &modifiedUnix)
		} else {
			var createdUnix int64

			err = rows.Scan(&page.Id, &page.Name, &page.Title, &page.Type, &page.Summary, &createdUnix, &publishedUnix, &modifiedUnix, &page.Text)

			page.Created = importTime(createdUnix)
		}
		checkError(err, "failed to scan page info")

		page.Published = importTime(publishedUnix)
		page.Modified = importTime(modifiedUnix)

		pages = append(pages, page)
	}

	return pages
}

func (p *Page) Typename() string {
	return PageTypeNames[p.Type]
}

func (p *Page) Url() string {
	switch p.Type {
	case PAGE_TYPE_POST:
		return p.Published.Format("/2006/01/") + p.Name
	case PAGE_TYPE_STATIC:
		return "/" + p.Name
	default:
		raiseError("unknown page type while generating url")
		// We will never get here.
		return ""
	}
}

// LastModified returns the HTTP Last-Modified time, which is the last time
// anything got changed on this object.
func (p *Page) LastModified() time.Time {
	// The time of publication is the last-modified time (when there is a
	// published time). So, the publication time should be set to the real
	// publication time, not a time in the future.
	return lastTime(p.Published, p.Modified)
}

func (p *Page) Update(blog *Blog, name, title, summary, text string) {
	p.Name = name
	p.Title = title
	p.Summary = summary
	p.Text = text
	p.Modified = time.Now()

	if p.Id == 0 {
		if p.Type == PAGE_TYPE_NONE {
			raiseError("type is not defined while inserting page")
		}

		p.Created = p.Modified

		result, err := blog.db.Exec("INSERT INTO pages (name, title, type, summary, text, created, modified) VALUES (?, ?, ?, ?, ?, ?, ?)",
			p.Name, p.Title, p.Type, p.Summary, p.Text, exportTime(p.Created), exportTime(p.Modified))
		checkError(err, "could not insert page")

		p.Id, err = result.LastInsertId()
		checkError(err, "could not get last inserted ID")
		if p.Id <= 0 {
			raiseError("page ID <= 0")
		}

	} else {
		_, err := blog.db.Exec("UPDATE pages SET name=?, title=?, summary=?, text=?, modified=? WHERE id=?",
			p.Name, p.Title, p.Summary, p.Text, exportTime(p.Modified), p.Id)
		checkError(err, "could not update page")
	}
}

// Publish updates the published time, making this page visible worldwide.
func (p *Page) Publish(blog *Blog) {
	p.Published = time.Now()

	_, err := blog.db.Exec("UPDATE Pages SET published=? WHERE id=?", exportTime(p.Published), p.Id)
	checkError(err, "could not publish page")
}

// Unpublish undoes publishing. It resets the published time to zero.
func (p *Page) Unpublish(blog *Blog) {
	p.Published = time.Time{} // nil value

	// We could also just simply set to 0
	_, err := blog.db.Exec("UPDATE Pages SET published=? WHERE id=?", exportTime(p.Published), p.Id)
	checkError(err, "could not unpublish page")
}

// LastModified returns the latest Last-Modified date for all pages.
func (ps Pages) LastModified() time.Time {
	// Modification time is independent of publication time, so it is required
	// to iterate over all pages to calculate the last modification time.
	var lm time.Time
	for i := 0; i < len(ps); i++ {
		if ps[i].LastModified().After(lm) {
			lm = ps[i].LastModified()
		}
	}

	return lm
}

// authenticated user
type User struct {
	name  string
	email string
}

var ErrInvalidUser = errors.New("User: invalid username or password")
var ErrInvalidToken = errors.New("User: invalid token")
var ErrExpiredToken = errors.New("User: token expired")
var ErrRedirect = errors.New("User: do a redirect") // error is returned when a redirect is needed

type UserToken struct {
	Email   string `json:"email"`
	Created int64  `json:"created"`
	Expires int64  `json:"expires"`
}

const TokenMaxAge = 60 * 60 * 24 * 7 // one week

// Returns a logged-in user, ErrInvalidUser, ErrInvalidToken, ErrExpiredToken,
// or ErrRedirect
func NewUser(blog *Blog, w http.ResponseWriter, r *http.Request) (*User, error) {
	if r.Method == "POST" && r.PostFormValue("login") != "" {
		row := blog.db.QueryRow("SELECT email,passwordHash FROM users WHERE email=?", r.PostFormValue("email"))

		var email, passwordHash string

		err := row.Scan(&email, &passwordHash)
		if err == sql.ErrNoRows {
			// no user with this email address
			return nil, ErrInvalidUser
		}
		checkError(err, "could not fetch information about user")

		if !verifyPassword(r.PostFormValue("password"), passwordHash) {
			// password doesn't match
			return nil, ErrInvalidUser
		}

		token, err := blog.SessionStore().NewToken(email)
		checkError(err, "could not create token")

		cookie := token.Cookie()
		cookie.Secure = blog.Secure

		h := w.Header()
		h.Set("Set-Cookie", cookie.String())
		h.Set("Location", r.URL.String())
		h.Set("Content-Length", "0")
		w.WriteHeader(303)
		return nil, ErrRedirect
	}

	if tokenCookie, err := r.Cookie(south.DefaultCookieName); err != http.ErrNoCookie {
		if err != nil {
			return nil, ErrInvalidToken
		}

		token, err := blog.SessionStore().Verify(tokenCookie)
		if err != nil {
			if err == south.ErrExpiredToken {
				return nil, ErrExpiredToken
			}
			return nil, ErrInvalidToken
		}

		u := User{}
		row := blog.db.QueryRow("SELECT fullname,email FROM users WHERE email=?", token.Id())
		err = row.Scan(&u.name, &u.email)
		checkError(err, "cannot fetch user from database")

		return &u, nil
	}

	return nil, nil
}

func (u *User) Name() string {
	return u.name
}

func (u *User) Email() string {
	return u.email
}
