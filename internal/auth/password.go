package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id, in the standard PHC string format, so the parameters travel with
// the hash and can be raised later without invalidating existing passwords.
//
//	$argon2id$v=19$m=65536,t=3,p=2$<salt-b64>$<hash-b64>

type Argon2Params struct {
	TimeCost   uint32
	MemoryKiB  uint32
	Threads    uint8
	SaltLength uint32
	KeyLength  uint32
}

func DefaultArgon2Params(timeCost, memoryKiB uint32, threads uint8) Argon2Params {
	return Argon2Params{
		TimeCost: timeCost, MemoryKiB: memoryKiB, Threads: threads,
		SaltLength: 16, KeyLength: 32,
	}
}

var (
	ErrPasswordTooShort = errors.New("password must be at least 8 characters")
	ErrPasswordTooLong  = errors.New("password must be at most 128 characters")
	ErrBadHash          = errors.New("stored password hash is malformed")
)

// MinPasswordLength is 8, matching the policy the reference implementation
// configured. Long passwords are capped because argon2 hashes the whole input
// and an unbounded password is a cheap way to burn server CPU.
const (
	MinPasswordLength = 8
	MaxPasswordLength = 128
)

func ValidatePassword(pw string) error {
	if len(pw) < MinPasswordLength {
		return ErrPasswordTooShort
	}
	if len(pw) > MaxPasswordLength {
		return ErrPasswordTooLong
	}
	return nil
}

// HashPassword returns a PHC-format argon2id hash.
func HashPassword(pw string, p Argon2Params) (string, error) {
	if err := ValidatePassword(pw); err != nil {
		return "", err
	}
	salt := make([]byte, p.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("auth: generate salt: %w", err)
	}
	key := argon2.IDKey([]byte(pw), salt, p.TimeCost, p.MemoryKiB, p.Threads, p.KeyLength)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, p.MemoryKiB, p.TimeCost, p.Threads,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword checks a password against a stored hash in constant time.
//
// It returns (false, nil) for a wrong password and (false, err) only when the
// stored hash cannot be parsed — the caller must treat both as a failed login
// and must not reveal which happened.
func VerifyPassword(pw, encoded string) (bool, error) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=..,t=..,p=..", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, fmt.Errorf("%w: unexpected format", ErrBadHash)
	}

	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil {
		return false, fmt.Errorf("%w: version: %w", ErrBadHash, err)
	}
	if version != argon2.Version {
		return false, fmt.Errorf("%w: argon2 version %d is not supported", ErrBadHash, version)
	}

	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, fmt.Errorf("%w: parameters: %w", ErrBadHash, err)
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, fmt.Errorf("%w: salt: %w", ErrBadHash, err)
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, fmt.Errorf("%w: hash: %w", ErrBadHash, err)
	}

	got := argon2.IDKey([]byte(pw), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}
