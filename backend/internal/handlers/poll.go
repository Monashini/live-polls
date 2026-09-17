package handlers

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"livepolls/internal/apperr"
	"livepolls/internal/config"
	"livepolls/internal/middleware"
	"livepolls/internal/services"
)

const (
	// voterCookieName holds the anonymous identity used to stop one browser
	// voting twice on the same poll.
	voterCookieName = "lp_voter"

	// One year. The cookie is not a session: someone who votes today should
	// still be recognised if they reopen the link next week.
	voterCookieMaxAge = 365 * 24 * 60 * 60

	// 16 random bytes -> 32 hex characters. Long enough that two voters never
	// collide, short enough to stay comfortable in a cookie.
	voterKeyBytes = 16
)

type PollHandler struct {
	polls *services.PollService
	cfg   *config.Config
}

func NewPollHandler(polls *services.PollService, cfg *config.Config) *PollHandler {
	return &PollHandler{polls: polls, cfg: cfg}
}

type createPollRequest struct {
	Question string   `json:"question"`
	Options  []string `json:"options"`
	// Mode is optional and defaults to "single" when omitted.
	Mode      string     `json:"mode"`
	ExpiresAt *time.Time `json:"expiresAt"`
}

type voteRequest struct {
	// A ballot is always a list, even in single-choice mode where it holds
	// one entry. One field for both modes beats two fields that can disagree,
	// and a nil slice is distinguishable from an empty selection.
	OptionIndexes []int `json:"optionIndexes"`
}

// POST /api/polls  (auth required)
func (h *PollHandler) Create(c *gin.Context) {
	ownerID, ok := middleware.UserID(c)
	if !ok {
		fail(c, apperr.Unauthorized("A valid access token is required."))
		return
	}

	var req createPollRequest
	if err := bindJSON(c, &req); err != nil {
		fail(c, err)
		return
	}

	poll, err := h.polls.Create(c.Request.Context(), ownerID, services.CreatePollInput{
		Question:  req.Question,
		Options:   req.Options,
		Mode:      req.Mode,
		ExpiresAt: req.ExpiresAt,
	})
	if err != nil {
		fail(c, err)
		return
	}

	respond(c, http.StatusCreated, gin.H{"poll": poll.View(time.Now().UTC())})
}

// GET /api/polls/:id  (public) -- accepts an ObjectID or a slug.
func (h *PollHandler) Get(c *gin.Context) {
	view, err := h.polls.Results(c.Request.Context(), c.Param("id"))
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, http.StatusOK, gin.H{"poll": view})
}

// GET /api/polls  (auth required) -- the caller's own polls.
func (h *PollHandler) ListMine(c *gin.Context) {
	ownerID, ok := middleware.UserID(c)
	if !ok {
		fail(c, apperr.Unauthorized("A valid access token is required."))
		return
	}

	views, err := h.polls.ListByOwner(c.Request.Context(), ownerID)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, http.StatusOK, gin.H{"polls": views})
}

// PATCH /api/polls/:id/close  (auth required, owner only)
func (h *PollHandler) Close(c *gin.Context) {
	ownerID, ok := middleware.UserID(c)
	if !ok {
		fail(c, apperr.Unauthorized("A valid access token is required."))
		return
	}

	view, err := h.polls.Close(c.Request.Context(), c.Param("id"), ownerID)
	if err != nil {
		fail(c, err)
		return
	}
	respond(c, http.StatusOK, gin.H{"poll": view})
}

// DELETE /api/polls/:id  (auth required, owner only)
func (h *PollHandler) Delete(c *gin.Context) {
	ownerID, ok := middleware.UserID(c)
	if !ok {
		fail(c, apperr.Unauthorized("A valid access token is required."))
		return
	}

	if err := h.polls.Delete(c.Request.Context(), c.Param("id"), ownerID); err != nil {
		fail(c, err)
		return
	}
	c.Status(http.StatusNoContent)
}

// POST /api/polls/:id/vote  (public)
func (h *PollHandler) Vote(c *gin.Context) {
	var req voteRequest
	if err := bindJSON(c, &req); err != nil {
		fail(c, err)
		return
	}
	if len(req.OptionIndexes) == 0 {
		fail(c, apperr.Validation("Please check the highlighted fields.", map[string]string{
			"optionIndexes": "select at least one choice",
		}))
		return
	}

	voterKey, err := h.voterKey(c)
	if err != nil {
		fail(c, err)
		return
	}

	view, voteErr := h.polls.Vote(
		c.Request.Context(),
		c.Param("id"),
		voterKey,
		h.hashIP(c.ClientIP()),
		req.OptionIndexes,
	)
	if voteErr != nil {
		fail(c, voteErr)
		return
	}

	respond(c, http.StatusCreated, gin.H{"poll": view})
}

// voterKey reads the anonymous voter cookie, issuing one if the browser does
// not have it yet.
//
// The cookie is HttpOnly, which is the reason for preferring it over
// localStorage: page JavaScript cannot read or rewrite it, so casually
// clearing it to vote again takes deliberate effort rather than one line in
// the console. It is still not a security control -- see the README.
func (h *PollHandler) voterKey(c *gin.Context) (string, error) {
	if existing, err := c.Cookie(voterCookieName); err == nil && isValidVoterKey(existing) {
		return existing, nil
	}

	buf := make([]byte, voterKeyBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", apperr.Internal(err)
	}
	key := hex.EncodeToString(buf)

	c.SetSameSite(sameSiteMode(h.cfg.CookieSameSite))
	c.SetCookie(
		voterCookieName,
		key,
		voterCookieMaxAge,
		"/",
		h.cfg.CookieDomain,
		h.cfg.CookieSecure,
		true, // HttpOnly
	)

	return key, nil
}

// isValidVoterKey rejects anything not in the exact shape we issue. A client
// can send any cookie value it likes; accepting arbitrary text would let one
// person pick a fresh key per request, and would put unbounded user input
// straight into a unique index.
func isValidVoterKey(v string) bool {
	if len(v) != voterKeyBytes*2 {
		return false
	}
	_, err := hex.DecodeString(v)
	return err == nil
}

// hashIP stores a salted digest instead of the address itself. The raw IP is
// personal data that this application has no reason to retain; the hash is
// still stable enough to spot abuse from one source later.
func (h *PollHandler) hashIP(ip string) string {
	if ip == "" {
		return ""
	}
	// Written through the streaming interface rather than
	// sha256.Sum256(append(salt, ip...)): append would write into the salt's
	// own backing array whenever it has spare capacity, quietly corrupting a
	// value shared by every request.
	hasher := sha256.New()
	hasher.Write(h.cfg.IPHashSalt)
	hasher.Write([]byte(ip))

	// 128 bits is far beyond collision risk for this purpose and halves the
	// stored size.
	return hex.EncodeToString(hasher.Sum(nil)[:16])
}

func sameSiteMode(name string) http.SameSite {
	switch name {
	case "none":
		return http.SameSiteNoneMode
	case "strict":
		return http.SameSiteStrictMode
	default:
		return http.SameSiteLaxMode
	}
}
