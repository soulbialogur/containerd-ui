package wsl

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)


var dangerousPathChars = regexp.MustCompile(`[;|&$` + "`" + `\\!\n\r\t ]`)
var shellMetacharacters = regexp.MustCompile(`[;|&$` + "`" + `(){}\\!\n\r\t]`)
func ValidatePath(path string) error {
	path = filepath.Clean(path)

	if path == "" {
		return fmt.Errorf("путь не может быть пустым")
	}

	if strings.Contains(path, "..") {
		return fmt.Errorf("путь содержит недопустимые компоненты «..»: %s", path)
	}

	if dangerousPathChars.MatchString(path) {
		return fmt.Errorf("путь содержит недопустимые символы: %s", path)
	}

	return nil
}

var containerIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.\-]*$`)

// ValidateContainerID проверяет ID контейнера на безопасность.
func ValidateContainerID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return fmt.Errorf("ID контейнера не может быть пустым")
	}

	if shellMetacharacters.MatchString(id) {
		return fmt.Errorf("ID контейнера содержит недопустимые символы: %s", id)
	}

	return nil
}

var imageRefPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.@/\-:]*$`)

// ValidateImageRef проверяет образ на безопасность.
func ValidateImageRef(image string) error {
	image = strings.TrimSpace(image)
	if image == "" {
		return fmt.Errorf("имя образа не может быть пустым")
	}

	if shellMetacharacters.MatchString(image) {
		return fmt.Errorf("образ содержит недопустимые символы: %s", image)
	}

	return nil
}
var networkNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.\-]*$`)

func ValidateNetworkName(name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("имя сети не может быть пустым")
	}

	if shellMetacharacters.MatchString(name) {
		return fmt.Errorf("имя сети содержит недопустимые символы: %s", name)
	}

	return nil
}

func ValidateComposePath(path string) error {
	return ValidatePath(path)
}
