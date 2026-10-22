package main

import "github.com/angvp/tango/i18n"

// registerCatalogs adds the example's Spanish for every page, form error and
// email the accounts app produces. tanGO ships English only; a host supplies
// the rest.
func registerCatalogs() {
	i18n.RegisterCatalog("es", spanish)
}

var spanish = map[string]string{
	"accounts.check_email.title":      "Revisa tu correo",
	"accounts.email.reset_body":       "Alguien pidió restablecer la contraseña de esta dirección de correo.\n\nPara elegir una nueva contraseña, abre este enlace en la próxima hora:\n\n%s\n\nSi no fuiste tú, ignora este mensaje: tu contraseña no cambia.\n",
	"accounts.email.reset_subject":    "Restablece tu contraseña",
	"accounts.email.verify_body":      "Confirma que esta es tu dirección de correo abriendo este enlace en las próximas 24 horas:\n\n%s\n\nSi no creaste una cuenta, ignora este mensaje.\n",
	"accounts.email.verify_subject":   "Confirma tu dirección de correo",
	"accounts.error.duplicate":        "Este correo ya está registrado.",
	"accounts.error.invalid_email":    "Introduce una dirección de correo válida.",
	"accounts.error.invalid_login":    "Correo o contraseña no válidos.",
	"accounts.error.password_long":    "La contraseña debe tener como máximo %d caracteres.",
	"accounts.error.password_short":   "La contraseña debe tener al menos %d caracteres.",
	"accounts.error.required":         "El correo y la contraseña son obligatorios.",
	"accounts.field.email":            "Correo",
	"accounts.field.new_password":     "Nueva contraseña",
	"accounts.field.password":         "Contraseña",
	"accounts.invalid_link.body":      "Puede que haya caducado, que ya se haya usado o que lo haya reemplazado uno más nuevo.",
	"accounts.invalid_link.heading":   "Este enlace no es válido",
	"accounts.invalid_link.title":     "Enlace no válido",
	"accounts.link.back_to_login":     "Volver a iniciar sesión",
	"accounts.login.register_link":    "¿Necesitas una cuenta? Regístrate",
	"accounts.login.submit":           "Iniciar sesión",
	"accounts.login.title":            "Iniciar sesión",
	"accounts.register.closed":        "El registro está cerrado por ahora.",
	"accounts.register.closed_title":  "Registro cerrado",
	"accounts.register.heading":       "Crear una cuenta",
	"accounts.register.login_link":    "¿Ya tienes una cuenta? Inicia sesión",
	"accounts.register.submit":        "Registrarse",
	"accounts.register.title":         "Registrarse",
	"accounts.reset.submit":           "Envíame un enlace",
	"accounts.reset.title":            "Restablece tu contraseña",
	"accounts.reset_confirm.submit":   "Guardar contraseña",
	"accounts.reset_confirm.title":    "Elige una nueva contraseña",
	"accounts.reset_sent.body":        "Si existe una cuenta con esa dirección, le hemos enviado un enlace para restablecer su contraseña. El enlace funciona durante 1 hora.",
	"accounts.unverified.body":        "Esta página necesita una dirección de correo confirmada. Abre el enlace que te enviamos o pide uno nuevo.",
	"accounts.unverified.heading":     "Confirma tu dirección de correo",
	"accounts.unverified.submit":      "Enviar un enlace nuevo",
	"accounts.verification_sent.body": "Si tu dirección aún necesita confirmación, te hemos enviado un enlace nuevo. Solo funciona el más reciente, durante 24 horas.",
	"accounts.verified.heading":       "Tu dirección de correo está confirmada.",
	"accounts.verified.title":         "Correo confirmado",
	"accounts.verify.submit":          "Confirmar mi correo",
	"accounts.verify.title":           "Confirma tu correo",
}
