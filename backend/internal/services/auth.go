package services

import (
	"context"
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"go.mongodb.org/mongo-driver/v2/bson"
	"golang.org/x/crypto/bcrypt"

	"livepolls/internal/apperr"
	"livepolls/internal/db"
	"livepolls/internal/models"
)

const (
	jwtIssuer = "livepolls"

	// bcrypt cost. 12 is roughly 250ms on current hardware: slow enough to
	// make offline cracking expensive, fast enough that login feels instant.
	bcryptCost = 12
)

type AuthService struct {
	users     *db.UserRepo
	jwtSecret []byte
	jwtTTL    time.Duration

	// dummyHash is compared against when an email does not exist, so a login
	// attempt costs the same whether or not the account is real. Without it,
	// response timing reveals which emails are registered.
	dummyHash []byte
}

func NewAuthService(users *db.UserRepo, jwtSecret []byte, jwtTTL time.Duration) (*AuthService, error) {
	dummy, err := bcrypt.GenerateFromPassword([]byte("not-a-real-password"), bcryptCost)
	if err != nil {
		return nil, err
	}
	return &AuthService{users: users, jwtSecret: jwtSecret, jwtTTL: jwtTTL, dummyHash: dummy}, nil
}

// Signup creates an account and returns it with a freshly signed token.
func (s *AuthService) Signup(ctx context.Context, email, password string) (*models.User, string, error) {
	normalized, err := ValidateCredentials(email, password)
	if err != nil {
		return nil, "", err
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		return nil, "", apperr.Internal(err)
	}

	user := &models.User{
		Email:        normalized,
		PasswordHash: string(hash),
		CreatedAt:    time.Now().UTC(),
	}

	switch err := s.users.Create(ctx, user); {
	case errors.Is(err, db.ErrDuplicate):
		// Deliberately the same wording a human would expect. This does leak
		// that the address is registered, which is unavoidable for a signup
		// form that has to tell you why it failed.
		return nil, "", apperr.Conflict(apperr.CodeConflict, "An account with that email already exists.")
	case err != nil:
		return nil, "", apperr.Internal(err)
	}

	token, err := s.issueToken(user)
	if err != nil {
		return nil, "", err
	}
	return user, token, nil
}

// Login verifies credentials. Every failure path returns the same message and
// status, so an attacker cannot distinguish "no such user" from "wrong password".
func (s *AuthService) Login(ctx context.Context, email, password string) (*models.User, string, error) {
	normalized, err := ValidateCredentials(email, password)
	if err != nil {
		// Even a malformed submission gets the generic message here: telling
		// someone their email "is not valid" on a login form is a free oracle.
		return nil, "", apperr.Unauthorized("Incorrect email or password.")
	}

	user, err := s.users.FindByEmail(ctx, normalized)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			// Burn the same amount of time a real comparison would take.
			_ = bcrypt.CompareHashAndPassword(s.dummyHash, []byte(password))
			return nil, "", apperr.Unauthorized("Incorrect email or password.")
		}
		return nil, "", apperr.Internal(err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(password)); err != nil {
		return nil, "", apperr.Unauthorized("Incorrect email or password.")
	}

	token, err := s.issueToken(user)
	if err != nil {
		return nil, "", err
	}
	return user, token, nil
}

// UserByID backs GET /api/auth/me.
func (s *AuthService) UserByID(ctx context.Context, id bson.ObjectID) (*models.User, error) {
	user, err := s.users.FindByID(ctx, id)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			// The token is validly signed but the account is gone, e.g. it was
			// deleted after the token was issued.
			return nil, apperr.Unauthorized("Your session is no longer valid.")
		}
		return nil, apperr.Internal(err)
	}
	return user, nil
}

func (s *AuthService) issueToken(u *models.User) (string, error) {
	now := time.Now().UTC()

	claims := jwt.RegisteredClaims{
		Subject:   u.ID.Hex(),
		Issuer:    jwtIssuer,
		IssuedAt:  jwt.NewNumericDate(now),
		NotBefore: jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(s.jwtTTL)),
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(s.jwtSecret)
	if err != nil {
		return "", apperr.Internal(err)
	}
	return signed, nil
}

// ParseToken validates a bearer token and returns the user ID it names.
//
// jwt.WithValidMethods is the critical line. Without it, an attacker can hand
// us a token whose header says alg:"none" or alg:"RS256" and the library would
// try to honour it. Pinning HS256 makes the algorithm our decision, not the
// token's.
func (s *AuthService) ParseToken(raw string) (bson.ObjectID, error) {
	var claims jwt.RegisteredClaims

	_, err := jwt.ParseWithClaims(raw, &claims,
		func(t *jwt.Token) (interface{}, error) { return s.jwtSecret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(jwtIssuer),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return bson.NilObjectID, apperr.Unauthorized("Your session has expired or is invalid.")
	}

	id, err := bson.ObjectIDFromHex(claims.Subject)
	if err != nil {
		return bson.NilObjectID, apperr.Unauthorized("Your session is malformed.")
	}
	return id, nil
}

// TokenTTLSeconds lets handlers tell the client when to re-authenticate
// without exposing the raw duration type.
func (s *AuthService) TokenTTLSeconds() int { return int(s.jwtTTL.Seconds()) }
