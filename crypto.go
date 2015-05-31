package main

import (
	"code.google.com/p/go.crypto/bcrypt"
	"github.com/aykevl/south"
)

func storePassword(password string) string {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	checkError(err, "cannot generate hash from password")
	return string(hash)
}

func verifyPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	if err == nil {
		return true
	} else if err == bcrypt.ErrMismatchedHashAndPassword {
		return false
	} else {
		internalError("cannot verify password", err, true)
		panic("unreachable")
	}
}

func generateSessionKey(ctx *Context) {
	sessionKey, err := south.GenerateKey()
	checkError(err, "could not generate session key")
	ctx.SessionKey = sessionKey
	ctx.Config.Update()
}
