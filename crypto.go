package main

import (
	"code.google.com/p/go.crypto/bcrypt"
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
