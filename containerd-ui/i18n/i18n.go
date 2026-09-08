package i18n

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed ru.json
var ruJSON []byte

//go:embed en.json
var enJSON []byte

type Locale string

const (
	LocaleRU Locale = "ru"
	LocaleEN Locale = "en"
)

var (
	translations map[Locale]map[string]interface{}
	currentLocale Locale
	localeMu     sync.RWMutex
)

func init() {
	translations = make(map[Locale]map[string]interface{})
	translations[LocaleRU] = loadJSON(ruJSON)
	translations[LocaleEN] = loadJSON(enJSON)
	currentLocale = LocaleRU
}

func loadJSON(data []byte) map[string]interface{} {
	var result map[string]interface{}
	if err := json.Unmarshal(data, &result); err != nil {
		panic(fmt.Sprintf("failed to load translations: %v", err))
	}
	return result
}

// T переводит ключ с поддержкой форматирования (printf-style)
func T(key string, args ...interface{}) string {
	localeMu.RLock()
	locale := currentLocale
	localeMu.RUnlock()

	value := lookup(key, locale)
	if value == "" {
		return key
	}

	if len(args) > 0 {
		return fmt.Sprintf(value, args...)
	}
	return value
}

// lookup рекурсивно ищет ключ вложенной структуры
func lookup(key string, locale Locale) string {
	parts := splitKey(key)
	current := translations[locale]

	for _, part := range parts {
		if current == nil {
			return ""
		}
		val, ok := current[part]
		if !ok {
			return ""
		}
		switch v := val.(type) {
		case string:
			return v
		case map[string]interface{}:
			current = v
		default:
			return ""
		}
	}
	return ""
}

func splitKey(key string) []string {
	result := make([]string, 0)
	current := ""
	for _, ch := range key {
		if ch == '.' {
			if current != "" {
				result = append(result, current)
				current = ""
			}
		} else {
			current += string(ch)
		}
	}
	if current != "" {
		result = append(result, current)
	}
	return result
}

// SetLocale устанавливает текущий язык
func SetLocale(locale Locale) {
	localeMu.Lock()
	defer localeMu.Unlock()
	if _, exists := translations[locale]; exists {
		currentLocale = locale
	}
}

// GetCurrentLocale возвращает текущий язык
func GetCurrentLocale() Locale {
	localeMu.RLock()
	defer localeMu.RUnlock()
	return currentLocale
}

// GetAvailableLocales возвращает список доступных языков
func GetAvailableLocales() []Locale {
	return []Locale{LocaleRU, LocaleEN}
}

// GetLocaleName возвращает название языка
func GetLocaleName(locale Locale) string {
	return T("app.language")
}
