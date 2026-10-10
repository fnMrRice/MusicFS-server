package mediastore

import "strings"

// CanonicalLanguage unifies declared ISO 639 aliases without inferring a
// language from text. Artwork's empty language remains a separate field slot.
func CanonicalLanguage(field, language string) string {
	language = strings.ToLower(language)
	if field != "lyrics" {
		return language
	}
	aliases := map[string]string{"eng": "en", "zho": "zh", "chi": "zh", "jpn": "ja", "kor": "ko", "fra": "fr", "fre": "fr", "deu": "de", "ger": "de", "spa": "es", "ita": "it", "por": "pt", "rus": "ru", "ara": "ar", "hin": "hi"}
	primary, rest, extended := strings.Cut(language, "-")
	if value, ok := aliases[primary]; ok {
		if extended {
			return value + "-" + rest
		}
		return value
	}
	if language == "" {
		return "und"
	}
	return language
}
