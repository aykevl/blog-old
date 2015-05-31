package main

import (
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"net/url"
	"os"
	"path"
	"syscall"

	"github.com/aykevl/south"
)

const CONFIG_PATH = "/etc/blog.json"
const DB_PATH = "/data/blog.sqlite3"
const IMPORT_PATH = "github.com/aykevl/blog"
const FCGI_PATH = "/.blog-fcgi.sock"

type Config struct {
	// non-configuration variables
	path string
	stat os.FileInfo

	ConfigData

	// Some fields derived from config data
	OriginURL  *url.URL // Parsed Origin field
	SessionKey []byte   // Decoded SessionKey
}

type ConfigData struct {
	// configuration variables
	Skin               string `json:"skin"`             // skin, default is "base"
	SiteTitle          string `json:"title"`            // blog title, default is "Blog"
	WebRoot            string `json:"webroot"`          // like "/var/www"
	BlogPath           string `json:"blogpath"`         // full path of source directory
	URLPrefix          string `json:"urlprefix"`        // for example "/blog", may be empty (default)
	Origin             string `json:"origin"`           // start of URL, for example "http://example.com"
	Secure             bool   `json:"secure"`           // all requests go over a secure connection
	HSTSMaxAge         int    `json:"hsts-max-age"`     // HTTP Strict Transport Security max-age (in seconds, 0 to disable)
	DatabaseType       string `json:"database-type"`    // for example "sqlite3"
	DatabaseConnection string `json:"database-connect"` // for example path to sqlite3 file
	SessionKey         string `json:"sessionkey"`       // 32-byte random base64-encoded key used to sign session cookies
	FastCGISocketPath  string `json:"fcgi-path"`        // FastCGI socket path
}

func loadConfig(root string) *Config {
	var c Config
	var err error

	// Defaults
	c.Skin = "base"
	c.SiteTitle = "Blog"
	c.Secure = true
	c.HSTSMaxAge = 15552000 // 180 days
	c.DatabaseType = "sqlite3"
	c.DatabaseConnection = root + DB_PATH
	c.BlogPath = root + "/src/" + IMPORT_PATH
	c.FastCGISocketPath = root + FCGI_PATH

	c.load(root)

	c.SessionKey = decodeKey(c.ConfigData.SessionKey)
	c.OriginURL, err = url.Parse(c.Origin)
	checkError(err, "could not parse origin URL in config")

	return &c
}

func (c *Config) load(root string) {
	var err error

	c.path = root + CONFIG_PATH

	f, err := os.Open(c.path)
	if os.IsNotExist(err) {
		return
	} else {
		checkError(err, "failed to open configuration file")
	}

	c.stat, err = f.Stat()
	checkError(err, "failed to stat configuration file")

	buf, err := ioutil.ReadAll(f)
	checkError(err, "failed to read configuration file")

	err = json.Unmarshal(buf, &c.ConfigData)
	checkError(err, "failed to parse configuration file")
}

func (c *Config) Update() {
	c.ConfigData.SessionKey = encodeKey(c.SessionKey)
	c.save()
}

func (c *Config) save() {
	out, err := json.MarshalIndent(c.ConfigData, "", "\t")
	checkError(err, "error while serializing JSON")

	err = os.MkdirAll(path.Dir(c.path), 0777)
	checkError(err, "could not create parent directory 'etc'")

	perm := os.FileMode(0600)
	var uid, gid int
	if c.stat != nil {
		st := c.stat.Sys()
		switch st := st.(type) {
		case *syscall.Stat_t:
			perm = os.FileMode(st.Mode) & os.ModePerm
			// This conversion is necessary as Chown takes ints and st.Xid are
			// uint32 types.
			uid = int(st.Uid)
			gid = int(st.Gid)
		}
	}

	f, err := os.OpenFile(c.path+".tmp", os.O_WRONLY|os.O_CREATE|os.O_TRUNC, perm)
	checkError(err, "could not open temporary config file")
	n, err := f.Write(out)
	checkError(err, "error while writing temporary config file")
	if n != len(out) {
		raiseError("could not write all config data to temporary config file")
	}

	if uid != 0 && gid != 0 {
		// reset uid and gid to what they were before
		err = f.Chown(uid, gid)
		checkWarning(err, "could not chown temporary config file")
	}

	checkError(f.Sync(), "error syncing temporary config file")
	checkError(f.Close(), "error closing temporary config file")

	err = os.Rename(c.path+".tmp", c.path)
	checkError(err, "error while renaming config file")
}

func encodeKey(key []byte) string {
	return base64.StdEncoding.EncodeToString(key[:])
}

func decodeKey(encodedKey string) []byte {
	if len(encodedKey) == 0 {
		return nil
	}
	key := make([]byte, base64.StdEncoding.DecodedLen(len(encodedKey)))
	_, err := base64.StdEncoding.Decode(key, []byte(encodedKey))
	checkError(err, "could not decode key")
	return key[:south.KeySize]
}
