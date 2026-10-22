package accounts_test

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/angvp/tango/i18n"
	"github.com/angvp/tango/i18n/i18ntest"
	"github.com/angvp/tango/mail/mailtest"
)

// inLocale serves handler with locale already resolved on every request, the
// way i18n.Middleware leaves it.
func inLocale(handler http.Handler, locale string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handler.ServeHTTP(w, r.WithContext(i18n.WithLocale(r.Context(), locale)))
	})
}

func getPage(handler http.Handler, path string) string {
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, nil))
	return response.Body.String()
}

func TestPagesAreUnchangedWithoutACatalog(t *testing.T) {
	i18ntest.Isolate(t)
	handler, _ := buildRegisterTestHandler(t)
	body := getPage(handler, "/accounts/login/")
	for _, want := range []string{"<html>\n", "<title>Log in</title>", "<h1>Log in</h1>", ">Need an account? Register</a>", `<button type="submit">Log in</button>`} {
		if !strings.Contains(body, want) {
			t.Errorf("login page missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "lang=") {
		t.Errorf("login page declares a language with nothing wired:\n%s", body)
	}
	// A locale that selects no catalog is the same page.
	if other := getPage(inLocale(handler, "qq-none"), "/accounts/login/"); !strings.Contains(other, "<html>\n") {
		t.Errorf("unmatched locale changed the page:\n%s", other)
	}
}

func TestPagesAndFormErrorsTranslate(t *testing.T) {
	i18ntest.Isolate(t)
	i18n.RegisterCatalog("zz-acc", map[string]string{
		"accounts.login.title":          "Entrar",
		"accounts.login.submit":         "Acceder",
		"accounts.field.email":          "Correo",
		"accounts.register.heading":     "Crear una cuenta",
		"accounts.error.invalid_login":  "Correo o contraseña no válidos.",
		"accounts.error.password_short": "La contraseña necesita al menos %d caracteres.",
		"accounts.error.duplicate":      "Este correo ya está registrado.",
		"accounts.error.required":       "Correo y contraseña son obligatorios.",
		"accounts.invalid_link.heading": "Este enlace no es válido",
	})
	handler, _ := buildRegisterTestHandler(t)
	es := inLocale(handler, "zz-acc")

	login := getPage(es, "/accounts/login/")
	for _, want := range []string{`<html lang="zz-acc">`, "<title>Entrar</title>", ">Correo</label>", `>Acceder</button>`} {
		if !strings.Contains(login, want) {
			t.Errorf("login page missing %q:\n%s", want, login)
		}
	}
	if !strings.Contains(getPage(es, "/accounts/register/"), "<h1>Crear una cuenta</h1>") {
		t.Error("register heading not translated")
	}

	short := postRegister(t, es, url.Values{"email": {"a@example.com"}, "password": {"short"}})
	if short.Code != http.StatusBadRequest || !strings.Contains(short.Body.String(), "La contraseña necesita al menos 8 caracteres.") {
		t.Errorf("short password: %d %s", short.Code, short.Body.String())
	}
	if empty := postRegister(t, es, url.Values{"email": {"a@example.com"}}); !strings.Contains(empty.Body.String(), "Correo y contraseña son obligatorios.") {
		t.Errorf("required message not translated: %s", empty.Body.String())
	}
	registerAccount(t, handler, "dup@example.com", "correct-password")
	if dup := postRegister(t, es, url.Values{"email": {"dup@example.com"}, "password": {"correct-password"}}); !strings.Contains(dup.Body.String(), "Este correo ya está registrado.") {
		t.Errorf("duplicate message not translated: %s", dup.Body.String())
	}
}

func TestGenericLoginErrorIsOneMessageInEveryLocale(t *testing.T) {
	i18ntest.Isolate(t)
	i18n.RegisterCatalog("zz-login", map[string]string{"accounts.error.invalid_login": "Datos no válidos."})
	handler, _ := buildRegisterTestHandler(t)
	registerAccount(t, handler, "known@example.com", "correct-password")
	es := inLocale(handler, "zz-login")

	unknown := postLogin(t, es, url.Values{"email": {"nobody@example.com"}, "password": {"whatever-password"}})
	wrong := postLogin(t, es, url.Values{"email": {"known@example.com"}, "password": {"wrong-password"}})
	for name, response := range map[string]*httptest.ResponseRecorder{"unknown": unknown, "wrong": wrong} {
		if response.Code != http.StatusUnauthorized || !strings.Contains(response.Body.String(), "Datos no válidos.") {
			t.Errorf("%s: %d %s", name, response.Code, response.Body.String())
		}
	}
}

func TestJSONErrorTextStaysEnglish(t *testing.T) {
	i18ntest.Isolate(t)
	i18n.RegisterCatalog("zz-json", map[string]string{
		"accounts.error.invalid_login":  "Datos no válidos.",
		"accounts.error.password_short": "Corta %d.",
	})
	site := newJSONSite(t, &fakeAuth{})
	site.createAccount("alice@example.com", true)
	inZZ := func(r *http.Request) { *r = *r.WithContext(i18n.WithLocale(r.Context(), "zz-json")) }

	response := site.postJSON("/accounts/api/login/", `{"identifier":"alice@example.com","password":"wrong-password"}`, inZZ)
	if body := response.Body.String(); !strings.Contains(body, "Invalid email or password.") || strings.Contains(body, "Datos") {
		t.Errorf("JSON login error not English: %d %s", response.Code, body)
	}
	response = site.postJSON("/accounts/api/register/", `{"email":"bob@example.com","password":"short"}`, inZZ)
	if body := response.Body.String(); !strings.Contains(body, "Password must be at least 8 characters") || strings.Contains(body, "Corta") {
		t.Errorf("JSON password problem not English: %d %s", response.Code, body)
	}
}

func inZZ(locale string) func(*http.Request) {
	return func(r *http.Request) { *r = *r.WithContext(i18n.WithLocale(r.Context(), locale)) }
}

func TestResetEmailFollowsTheLanguageOfTheRequestThatAskedForIt(t *testing.T) {
	i18ntest.Isolate(t)
	i18n.RegisterCatalog("zz-mail", map[string]string{
		"accounts.email.reset_subject": "Restablece tu contraseña",
		"accounts.email.reset_body":    "Abre este enlace en 1 hora:\n\n%s\n",
	})
	site := newMailSite(t, &mailtest.Sender{}, true)
	site.createAccount("alice@example.com", true)
	site.start()
	defer site.stop()

	site.askForReset("alice@example.com", inZZ("ZZ_mail"))
	message := site.sent(1)[0]
	if message.Subject != "Restablece tu contraseña" || !strings.Contains(message.Text, "Abre este enlace en 1 hora:") {
		t.Fatalf("email not translated: %+v", message)
	}
	tokenIn(t, message, "https://example.com/accounts/password-reset/confirm/")
}

func TestEmailWithoutAResolvedLocaleIsEnglish(t *testing.T) {
	i18ntest.Isolate(t)
	i18n.RegisterCatalog("zz-mail", map[string]string{"accounts.email.reset_subject": "Restablece"})
	site := newMailSite(t, &mailtest.Sender{}, true)
	site.createAccount("alice@example.com", true)
	site.start()
	defer site.stop()

	site.askForReset("alice@example.com")
	if got := site.sent(1)[0].Subject; got != "Reset your password" {
		t.Fatalf("subject = %q, want English", got)
	}
}

func TestEmailBodyThatLostItsLinkFallsBackToEnglish(t *testing.T) {
	i18ntest.Isolate(t)
	i18n.RegisterCatalog("zz-mail", map[string]string{"accounts.email.reset_body": "Sin enlace"})
	site := newMailSite(t, &mailtest.Sender{}, true)
	site.createAccount("alice@example.com", true)
	site.start()
	defer site.stop()

	site.askForReset("alice@example.com", inZZ("zz-mail"))
	message := site.sent(1)[0]
	if strings.Contains(message.Text, "Sin enlace") || !strings.Contains(message.Text, "?token=") {
		t.Fatalf("body = %q, want the English body with its link", message.Text)
	}
}
