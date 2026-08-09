package integration

import (
	"net/http"
	"strings"
	"testing"

	"github.com/ceserve/courier-os/tests/harness"
)

func TestLoginSucceedsAndIssuesUsableToken(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "AUTH1"})

	resp := env.Do(t, "GET", "/api/v1/auth/me", tn.AdminAccessTok, nil)
	if resp.Status != http.StatusOK {
		t.Fatalf("expected 200 from /auth/me, got %d: %s", resp.Status, resp.Raw)
	}
	if got := resp.Body["email"]; got != tn.AdminEmail {
		t.Errorf("expected email %q, got %v", tn.AdminEmail, got)
	}
	org, _ := resp.Body["organization"].(map[string]any)
	if org["id"] != tn.OrgPublicID {
		t.Errorf("expected organization %q, got %v", tn.OrgPublicID, org["id"])
	}
}

// TestLoginDoesNotRevealAccountExistence is the account-enumeration control:
// an unknown email and a wrong password must be indistinguishable.
func TestLoginDoesNotRevealAccountExistence(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "AUTH2"})

	unknown := env.Do(t, "POST", "/api/v1/auth/login", "", map[string]any{
		"email": "nobody@example.test", "password": "WrongPassw0rd!",
	})
	wrongPassword := env.Do(t, "POST", "/api/v1/auth/login", "", map[string]any{
		"email": tn.AdminEmail, "password": "DefinitelyWrong1!",
	})

	if unknown.Status != http.StatusUnauthorized || wrongPassword.Status != http.StatusUnauthorized {
		t.Fatalf("expected both to be 401, got %d and %d", unknown.Status, wrongPassword.Status)
	}
	if unknown.ErrorCode() != wrongPassword.ErrorCode() {
		t.Errorf("error codes differ and leak account existence: %q vs %q",
			unknown.ErrorCode(), wrongPassword.ErrorCode())
	}
	unknownMsg, _ := unknown.Body["error"].(map[string]any)
	wrongMsg, _ := wrongPassword.Body["error"].(map[string]any)
	if unknownMsg["message"] != wrongMsg["message"] {
		t.Errorf("error messages differ and leak account existence: %v vs %v",
			unknownMsg["message"], wrongMsg["message"])
	}
}

// TestAccountLocksAfterRepeatedFailures proves the brute-force control.
func TestAccountLocksAfterRepeatedFailures(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "AUTH3"})

	maxFailures := env.Config.Auth.MaxFailedLogins
	for i := 0; i < maxFailures; i++ {
		env.Do(t, "POST", "/api/v1/auth/login", "", map[string]any{
			"email": tn.AdminEmail, "password": "WrongPassw0rd!",
		})
	}
	// The correct password must now be refused: the account is locked.
	resp := env.Do(t, "POST", "/api/v1/auth/login", "", map[string]any{
		"email": tn.AdminEmail, "password": tn.AdminPassword,
	})
	if resp.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401 after lockout, got %d: %s", resp.Status, resp.Raw)
	}
	if code := resp.ErrorCode(); code != "ACCOUNT_LOCKED" {
		t.Errorf("expected ACCOUNT_LOCKED, got %q", code)
	}
}

// TestRefreshRotatesAndDetectsReuse proves refresh-token rotation with reuse
// detection: presenting a rotated token kills the whole chain.
func TestRefreshRotatesAndDetectsReuse(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "AUTH4"})

	login := env.Do(t, "POST", "/api/v1/auth/login", "", map[string]any{
		"email": tn.AdminEmail, "password": tn.AdminPassword,
	})
	tokens, _ := login.Body["tokens"].(map[string]any)
	firstRefresh, _ := tokens["refreshToken"].(string)

	rotated := env.Do(t, "POST", "/api/v1/auth/refresh", "", map[string]any{
		"refreshToken": firstRefresh,
	})
	if rotated.Status != http.StatusOK {
		t.Fatalf("expected refresh to succeed, got %d: %s", rotated.Status, rotated.Raw)
	}
	secondRefresh, _ := rotated.Body["refreshToken"].(string)
	secondAccess, _ := rotated.Body["accessToken"].(string)
	if secondRefresh == firstRefresh {
		t.Fatal("refresh token was not rotated")
	}
	if env.Do(t, "GET", "/api/v1/auth/me", secondAccess, nil).Status != http.StatusOK {
		t.Fatal("rotated access token should authenticate")
	}

	// Replaying the original token is the leak signal.
	replay := env.Do(t, "POST", "/api/v1/auth/refresh", "", map[string]any{
		"refreshToken": firstRefresh,
	})
	if replay.Status != http.StatusUnauthorized || replay.ErrorCode() != "TOKEN_REVOKED" {
		t.Fatalf("expected TOKEN_REVOKED on reuse, got %d %s", replay.Status, replay.ErrorCode())
	}
	// The whole chain must now be dead, including the token issued by the
	// legitimate rotation.
	afterReuse := env.Do(t, "POST", "/api/v1/auth/refresh", "", map[string]any{
		"refreshToken": secondRefresh,
	})
	if afterReuse.Status != http.StatusUnauthorized {
		t.Fatalf("expected the rotated chain to be revoked after reuse detection, got %d: %s",
			afterReuse.Status, afterReuse.Raw)
	}
}

// TestLogoutRevokesSessionImmediately proves revocation is not delayed by the
// session cache.
func TestLogoutRevokesSessionImmediately(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "AUTH5"})

	// Warm the session cache.
	if env.Do(t, "GET", "/api/v1/auth/me", tn.AdminAccessTok, nil).Status != http.StatusOK {
		t.Fatal("expected authenticated request to succeed before logout")
	}
	if resp := env.Do(t, "POST", "/api/v1/auth/logout", tn.AdminAccessTok, map[string]any{}); resp.Status != http.StatusNoContent {
		t.Fatalf("expected 204 from logout, got %d: %s", resp.Status, resp.Raw)
	}
	after := env.Do(t, "GET", "/api/v1/auth/me", tn.AdminAccessTok, nil)
	if after.Status != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d: %s", after.Status, after.Raw)
	}
}

// TestPasswordResetIsSingleUse proves the reset token cannot be replayed.
func TestPasswordResetIsSingleUse(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)
	geo := env.Geography(t)
	tn := env.NewTenant(t, geo, harness.TenantOptions{Code: "AUTH6"})

	forgot := env.Do(t, "POST", "/api/v1/auth/forgot-password", "", map[string]any{
		"email": tn.AdminEmail,
	})
	if forgot.Status != http.StatusOK {
		t.Fatalf("expected 200 from forgot-password, got %d: %s", forgot.Status, forgot.Raw)
	}
	token, _ := forgot.Body["resetToken"].(string)
	if token == "" {
		t.Fatal("test configuration should expose resetToken outside production")
	}

	newPassword := "BrandNewPassw0rd!"
	first := env.Do(t, "POST", "/api/v1/auth/reset-password", "", map[string]any{
		"token": token, "newPassword": newPassword,
	})
	if first.Status != http.StatusOK {
		t.Fatalf("expected reset to succeed, got %d: %s", first.Status, first.Raw)
	}
	second := env.Do(t, "POST", "/api/v1/auth/reset-password", "", map[string]any{
		"token": token, "newPassword": "AnotherPassw0rd!",
	})
	if second.Status != http.StatusUnauthorized {
		t.Fatalf("expected the reset token to be single use, got %d: %s", second.Status, second.Raw)
	}

	// The new credential works and the old one does not.
	if env.Do(t, "POST", "/api/v1/auth/login", "", map[string]any{
		"email": tn.AdminEmail, "password": newPassword,
	}).Status != http.StatusOK {
		t.Error("expected login with the new password to succeed")
	}
	if env.Do(t, "POST", "/api/v1/auth/login", "", map[string]any{
		"email": tn.AdminEmail, "password": tn.AdminPassword,
	}).Status == http.StatusOK {
		t.Error("the old password must no longer authenticate")
	}
}

// TestForgotPasswordDoesNotRevealAccounts checks the enumeration control on the
// reset path.
func TestForgotPasswordDoesNotRevealAccounts(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)

	resp := env.Do(t, "POST", "/api/v1/auth/forgot-password", "", map[string]any{
		"email": "nobody-at-all@example.test",
	})
	if resp.Status != http.StatusOK {
		t.Fatalf("expected 200 for an unknown address, got %d: %s", resp.Status, resp.Raw)
	}
	if _, present := resp.Body["resetToken"]; present {
		t.Error("no reset token should be issued for an unknown address")
	}
	msg, _ := resp.Body["message"].(string)
	if !strings.Contains(msg, "If an account exists") {
		t.Errorf("expected a neutral message, got %q", msg)
	}
}

// TestMalformedTokenIsRejected covers the JWT verification path.
func TestMalformedTokenIsRejected(t *testing.T) {
	env := harness.Start(t)
	env.Reset(t)

	for name, token := range map[string]string{
		"garbage":        "not-a-token",
		"algorithm none": "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJzdWIiOiJ1c3JfMSJ9.",
		"empty":          "",
	} {
		t.Run(name, func(t *testing.T) {
			resp := env.Do(t, "GET", "/api/v1/auth/me", token, nil)
			if resp.Status != http.StatusUnauthorized {
				t.Fatalf("expected 401, got %d: %s", resp.Status, resp.Raw)
			}
		})
	}
}
