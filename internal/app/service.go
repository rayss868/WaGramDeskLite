//go:build linux

package app

// Service identifies which messenger an account serves.
type Service string

const (
	ServiceWhatsApp Service = "whatsapp"
	ServiceTelegram Service = "telegram"
)

func (s Service) normalize() Service {
	switch s {
	case ServiceTelegram:
		return ServiceTelegram
	default:
		return ServiceWhatsApp
	}
}

// serviceURL returns the web client URL for a service.
func serviceURL(s Service) string {
	if s.normalize() == ServiceTelegram {
		return "https://web.telegram.org/a/"
	}
	return "https://web.whatsapp.com"
}

// serviceLabel is the short display name used in the UI.
func serviceLabel(s Service) string {
	if s.normalize() == ServiceTelegram {
		return "Telegram"
	}
	return "WhatsApp"
}

// serviceBadge is the 1-2 letter mark shown in the overlay and tray.
func serviceBadge(s Service) string {
	if s.normalize() == ServiceTelegram {
		return "TG"
	}
	return "WA"
}
