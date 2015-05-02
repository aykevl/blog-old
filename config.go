package main

import (
	"encoding/base64"
	"encoding/json"
	"io/ioutil"
	"os"

	"github.com/aykevl/south"
)

const CONFIG_PATH = "/etc/blog.json"

type Config struct {
	// non-configuration variables
	configPath string

	ConfigData

	SessionKey []byte
}

type ConfigData struct {
	// configuration variables
	Skin               string `json:"skin"`
	SiteTitle          string `json:"title"`
	PostsDirectory     string `json:"posts"`
	WebRoot            string `json:"webroot"`
	BlogRoot           string `json:"blogroot"`
	SiteRoot           string `json:"siterooturl"`
	Origin             string `json:"origin"`
	Insecure           bool   `json:"insecure"`
	DatabaseType       string `json:"database-type"`
	DatabaseConnection string `json:"database-connect"`
	SessionKey         string `json:"sessionkey"`
}

func loadConfig(root string) *Config {
	var c Config
	c.load(root)

	c.SessionKey = decodeKey(c.ConfigData.SessionKey)

	// Defaults

	if c.SiteTitle == "" {
		c.SiteTitle = "Blog"
	}
	if c.DatabaseType == "" {
		c.DatabaseType = "sqlite3"
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
