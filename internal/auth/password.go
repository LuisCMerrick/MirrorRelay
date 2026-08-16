package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

const (
	argonTime    uint32 = 3
	argonMemory  uint32 = 64 * 1024
	argonKeyLen  uint32 = 32
	argonSaltLen        = 16
)

var argonSem = make(chan struct{}, 4)

func acquireArgon() func() {
	argonSem <- struct{}{}
	return func() { <-argonSem }
}

func HashPassword(password string) (string, error) {
	if len(password) < 10 {
		return "", errors.New("password must be at least 10 characters")
	}
	release := acquireArgon()
	defer release()

	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	threadCount := runtime.GOMAXPROCS(0)
	if threadCount > 4 {
		threadCount = 4
	}
	if threadCount < 1 {
		threadCount = 1
	}
	threads := uint8(threadCount)
	hash := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, threads, argonKeyLen)
	enc := base64.RawStdEncoding
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, threads, enc.EncodeToString(salt), enc.EncodeToString(hash)), nil
}

func VerifyPassword(encoded, password string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false
	}
	var memory, iterations uint64
	var threads uint64
	for _, item := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(item, "=")
		if !ok {
			return false
		}
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return false
		}
		switch key {
		case "m":
			memory = n
		case "t":
			iterations = n
		case "p":
			threads = n
		}
	}
	if memory < 8*1024 || memory > 1024*1024 || iterations == 0 || iterations > 10 || threads == 0 || threads > 32 {
		return false
	}
	release := acquireArgon()
	defer release()
	enc := base64.RawStdEncoding
	salt, err := enc.DecodeString(parts[4])
	if err != nil {
		return false
	}
	want, err := enc.DecodeString(parts[5])
	if err != nil || len(want) == 0 {
		return false
	}
	got := argon2.IDKey([]byte(password), salt, uint32(iterations), uint32(memory), uint8(threads), uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1
}
