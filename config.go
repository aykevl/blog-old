package main

import (
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"net/url"
	"os"
	"path"

	"github.com/aykevl/south"
)

const CONFIG_PATH = "/etc/blog.json"
const DB_PATH = "/data/blog.sqlite3"
const IMPORT_PATH = "github.com/aykevl/blog"
const FCGI_PATH = "/.blog-fcgi.sock"

type Config struct {
	// non-configuration variables
	configPath string

	ConfigData

	SessionKey []byte
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
	DatabaseType       string `json:"database-type"`    // for example "sqlite3"
	DatabaseConnection string `json:"database-connect"` // for example path to sqlite3 file
	SessionKey         string `json:"sessionkey"`       // 32-byte random base64-encoded key used to sign session cookies
	FastCGISocketPath  string `json:"fcgi-path"`        // FastCGI socket path

	OriginURL *url.URL // Parsed 'Origin' field
}

func loadConfig(root string) *Config {
	var c Config
	var err error

	// Defaults
	c.Skin = "base"
	c.SiteTitle = "Blog"
	c.Secure = true
	c.DatabaseType = "sqlite3"
	c.DatabaseConnection = root + DB_PATH
	c.BlogPath = root + "/src/" + IMPORT_PATH
	c.FastCGISocketPath = root + FCGI_PATH

	c.load(root)

	c.SessionKey = decodeKey(c.ConfigData.SessionKey)
	c.OriginURL, err = url.Parse(c.Origin)
	if err != nil {
		internalError("could not parse origin URL in config", err)
	}

	return &c
}

func (c *Config) load(root string) {
	c.configPath = root + CONFIG_PATH

	f, err := os.Open(c.configPath)
	if os.IsNotExist(err) {
		return
	} else {
		checkError(err, "failed to open configuration file")
	}

	buf, err := ioutil.ReadAll(f)
	checkError(err, "failed to read configuration file")

	err = json.Unmarshal(buf, &c.ConfigData)
	checkError(err, "failed to parse configuration file")
}

func (c *Config) Update() {
	c.ConfigData.SessionKey = encodeKey(c.SessionKey)

	out, err := json.MarshalIndent(c.ConfigData, "", "\t")
	checkError(err, "error while serializing JSON")

	err = os.MkdirAll(path.Dir(c.configPath), 0777)
	if err != nil {
		internalError("could not create parent directory 'etc'", err)
	}

	err = ioutil.WriteFile(c.configPath+".tmp", out, 0600)
	checkError(err, "error while writing temporary config file")

	err = os.Rename(c.configPath+".tmp", c.configPath)
	checkError(err, "error while writing renaming config file")
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
