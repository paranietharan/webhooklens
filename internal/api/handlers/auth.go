package handlers

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
	"webhooklens/internal/api/middleware"
	"webhooklens/internal/config"
	"webhooklens/internal/mailer"
	"webhooklens/internal/models"
	"webhooklens/internal/store"
)

type AuthHandler struct {
	store  store.Store
	cfg    *config.Config
	mailer mailer.Mailer
}

func NewAuthHandler(store store.Store, cfg *config.Config, mailer mailer.Mailer) *AuthHandler {
	return &AuthHandler{
		store:  store,
		cfg:    cfg,
		mailer: mailer,
	}
}

type registerRequest struct {
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
	Email     string `json:"email"`
	Password  string `json:"password"`
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type resendVerificationRequest struct {
	Email string `json:"email"`
}

type authResponse struct {
	Token string       `json:"token"`
	User  *models.User `json:"user"`
}

type registerResponse struct {
	Message string       `json:"message"`
	User    *models.User `json:"user"`
}

func (h *AuthHandler) Register(w http.ResponseWriter, r *http.Request) {
	var req registerRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.FirstName = strings.TrimSpace(req.FirstName)
	req.LastName = strings.TrimSpace(req.LastName)
	req.Email = strings.TrimSpace(req.Email)

	if req.FirstName == "" {
		respondError(w, http.StatusBadRequest, "first name is required")
		return
	}
	if req.LastName == "" {
		respondError(w, http.StatusBadRequest, "last name is required")
		return
	}
	if req.Email == "" || !strings.Contains(req.Email, "@") {
		respondError(w, http.StatusBadRequest, "valid email is required")
		return
	}
	if len(req.Password) < 8 {
		respondError(w, http.StatusBadRequest, "password must be at least 8 characters")
		return
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to process password")
		return
	}

	user := &models.User{
		ID:           uuid.New(),
		FirstName:    req.FirstName,
		LastName:     req.LastName,
		Email:        req.Email,
		PasswordHash: string(hash),
		IsVerified:   false,
		CreatedAt:    time.Now().UTC(),
	}

	if err := h.store.Users().Create(r.Context(), user); err != nil {
		if errors.Is(err, store.ErrDuplicateEmail) {
			respondError(w, http.StatusConflict, "email already registered")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to create user")
		return
	}

	// Generate 32-byte secure token for email verification
	tokenBytes := make([]byte, 32)
	_, _ = rand.Read(tokenBytes)
	verificationToken := hex.EncodeToString(tokenBytes)

	ev := &models.EmailVerification{
		ID:        uuid.New(),
		UserID:    user.ID,
		Token:     verificationToken,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
		CreatedAt: time.Now().UTC(),
	}

	if err := h.store.Users().CreateEmailVerification(r.Context(), ev); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate email verification")
		return
	}

	// Send verification email via Mailpit (non-blocking)
	go func() {
		if err := h.mailer.SendVerificationEmail(user.Email, user.FirstName, verificationToken); err != nil {
			log.Printf("[auth] failed to send verification email to %s: %v", user.Email, err)
		}
	}()

	respondJSON(w, http.StatusCreated, registerResponse{
		Message: "User registered successfully. A verification link has been sent to your email.",
		User:    user,
	})
}

func (h *AuthHandler) VerifyEmail(w http.ResponseWriter, r *http.Request) {
	token := r.URL.Query().Get("token")
	if token == "" {
		respondError(w, http.StatusBadRequest, "verification token is required")
		return
	}

	ev, err := h.store.Users().GetEmailVerificationByToken(r.Context(), token)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respondError(w, http.StatusBadRequest, "invalid or expired verification token")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to verify email")
		return
	}

	if time.Now().UTC().After(ev.ExpiresAt) {
		_ = h.store.Users().DeleteEmailVerification(r.Context(), token)
		respondError(w, http.StatusBadRequest, "verification token has expired")
		return
	}

	if err := h.store.Users().SetVerified(r.Context(), ev.UserID); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to activate account")
		return
	}

	_ = h.store.Users().DeleteEmailVerification(r.Context(), token)

	user, err := h.store.Users().GetByID(r.Context(), ev.UserID)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to load verified user profile")
		return
	}

	jwtToken, err := middleware.GenerateToken(user.ID, user.Email, h.cfg.JWTSecret, h.cfg.JWTExpiration)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "email verified, but token generation failed")
		return
	}

	respondJSON(w, http.StatusOK, map[string]any{
		"message": "Email verified successfully.",
		"token":   jwtToken,
		"user":    user,
	})
}

func (h *AuthHandler) ResendVerification(w http.ResponseWriter, r *http.Request) {
	var req resendVerificationRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" {
		respondError(w, http.StatusBadRequest, "email is required")
		return
	}

	user, err := h.store.Users().GetByEmail(r.Context(), req.Email)
	if err != nil {
		// Generic success response to prevent email enumeration
		respondJSON(w, http.StatusOK, map[string]string{
			"message": "If an unverified account with this email exists, a new verification link has been sent.",
		})
		return
	}

	if user.IsVerified {
		respondError(w, http.StatusBadRequest, "email is already verified")
		return
	}

	_ = h.store.Users().DeleteEmailVerificationsByUserID(r.Context(), user.ID)

	tokenBytes := make([]byte, 32)
	_, _ = rand.Read(tokenBytes)
	verificationToken := hex.EncodeToString(tokenBytes)

	ev := &models.EmailVerification{
		ID:        uuid.New(),
		UserID:    user.ID,
		Token:     verificationToken,
		ExpiresAt: time.Now().UTC().Add(24 * time.Hour),
		CreatedAt: time.Now().UTC(),
	}

	if err := h.store.Users().CreateEmailVerification(r.Context(), ev); err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate email verification")
		return
	}

	go func() {
		if err := h.mailer.SendVerificationEmail(user.Email, user.FirstName, verificationToken); err != nil {
			log.Printf("[auth] failed to send verification email to %s: %v", user.Email, err)
		}
	}()

	respondJSON(w, http.StatusOK, map[string]string{
		"message": "If an unverified account with this email exists, a new verification link has been sent.",
	})
}

func (h *AuthHandler) Login(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respondError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	req.Email = strings.TrimSpace(req.Email)
	if req.Email == "" || req.Password == "" {
		respondError(w, http.StatusBadRequest, "email and password are required")
		return
	}

	user, err := h.store.Users().GetByEmail(r.Context(), req.Email)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respondError(w, http.StatusUnauthorized, "invalid email or password")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to authenticate")
		return
	}

	if err := bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password)); err != nil {
		respondError(w, http.StatusUnauthorized, "invalid email or password")
		return
	}

	if !user.IsVerified {
		respondError(w, http.StatusForbidden, "email is not verified, please check your inbox or request a new verification link")
		return
	}

	token, err := middleware.GenerateToken(user.ID, user.Email, h.cfg.JWTSecret, h.cfg.JWTExpiration)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "failed to generate authentication token")
		return
	}

	respondJSON(w, http.StatusOK, authResponse{
		Token: token,
		User:  user,
	})
}

func (h *AuthHandler) Me(w http.ResponseWriter, r *http.Request) {
	userID, ok := middleware.GetUserID(r.Context())
	if !ok {
		respondError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.store.Users().GetByID(r.Context(), userID)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			respondError(w, http.StatusNotFound, "user not found")
			return
		}
		respondError(w, http.StatusInternalServerError, "failed to retrieve user profile")
		return
	}

	respondJSON(w, http.StatusOK, user)
}
