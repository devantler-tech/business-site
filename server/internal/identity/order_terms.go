package identity

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"unicode"
	"unicode/utf8"
)

var errOrderConfirmation = errors.New("order confirmation is unavailable")

type agreementTerms struct{ text [2]string }

// LinkOrderTerms installs complete EN/DA terms from the admitted serving snapshot.
// It neither enables ordering nor sends mail; absent terms keep new orders closed.
func (s *Foundation) LinkOrderTerms(en, da string) error {
	for _, text := range []string{en, da} {
		if text == "" || len(text) > 65536 || !utf8.ValidString(text) {
			return errOrderConfirmation
		}
		for _, r := range text {
			if (unicode.IsControl(r) && r != '\n') || unicode.Is(unicode.Cf, r) {
				return errOrderConfirmation
			}
		}
	}
	s.orderTerms.Store(&agreementTerms{[2]string{en, da}})
	return nil
}

func (s *Foundation) termsFor(lang string) (text, hash string) {
	terms := s.orderTerms.Load()
	if terms == nil {
		return "", ""
	}
	i := 0
	if lang == "da" {
		i = 1
	}
	text = terms.text[i]
	sum := sha256.Sum256([]byte(text))
	return text, hex.EncodeToString(sum[:])
}
