package handlers

import (
	"errors"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mohi/pms-marg-inspired/internal/models"
)

// respondError writes the uniform JSON error envelope used across the API.
func respondError(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"error": msg})
}

func respondBadRequest(c *gin.Context, err error) {
	respondError(c, http.StatusBadRequest, err.Error())
}

func respondConflict(c *gin.Context, msg string) {
	respondError(c, http.StatusConflict, msg)
}

func respondInternal(c *gin.Context, err error) {
	log.Printf("internal error on %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	respondError(c, http.StatusInternalServerError, "internal server error")
}

// respondStreamError handles a failure from a handler that streams its body
// (PDF downloads). If nothing was written yet the normal 500 envelope is
// still possible; once the status line is flushed only logging (plus abort)
// remains — writing a second status would corrupt the stream.
func respondStreamError(c *gin.Context, err error) {
	log.Printf("stream error on %s %s: %v", c.Request.Method, c.Request.URL.Path, err)
	if c.Writer.Written() {
		c.Abort()
		return
	}
	respondError(c, http.StatusInternalServerError, "internal server error")
}

func bindDateRange(c *gin.Context, defaultDays int) (time.Time, time.Time, error) {
	now := time.Now().UTC()
	end := now.AddDate(0, 0, 1)
	start := end.AddDate(0, 0, -defaultDays)

	if s := c.Query("start_date"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("start_date must be YYYY-MM-DD")
		}
		start = t.UTC()
	}
	if s := c.Query("end_date"); s != "" {
		t, err := time.Parse("2006-01-02", s)
		if err != nil {
			return time.Time{}, time.Time{}, errors.New("end_date must be YYYY-MM-DD")
		}
		end = t.UTC().AddDate(0, 0, 1)
	}
	if start.After(end) {
		return time.Time{}, time.Time{}, errors.New("start_date must not exceed end_date")
	}
	return start, end, nil
}

// mapRepoError converts domain sentinels into their HTTP counterparts.
func mapRepoError(c *gin.Context, err error) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, models.ErrNotFound):
		respondError(c, http.StatusNotFound, err.Error())
	case errors.Is(err, models.ErrOverpayment):
		respondBadRequest(c, err)
	case errors.Is(err, models.ErrCannotDisableOwner):
		respondBadRequest(c, err)
	case errors.Is(err, models.ErrEmployeeLimitReached),
		errors.Is(err, models.ErrRequestNotPending),
		errors.Is(err, models.ErrRequesterApprover),
		errors.Is(err, models.ErrStaleStock):
		respondConflict(c, err.Error())
	case errors.Is(err, models.ErrNotAMember):
		respondError(c, http.StatusForbidden, err.Error())
	case errors.Is(err, models.ErrDuplicate):
		respondConflict(c, err.Error())
	default:
		var valErr *models.ValidationError
		var stockErr *models.InsufficientStockError
		var creditErr *models.CreditLimitExceededError
		switch {
		case errors.As(err, &valErr), errors.As(err, &stockErr), errors.As(err, &creditErr):
			respondBadRequest(c, err)
		default:
			respondInternal(c, err)
		}
	}
	return true
}
