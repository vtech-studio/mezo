package main

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var brlPattern = regexp.MustCompile(`^(?:[0-9]+|[0-9]{1,3}(?:\.[0-9]{3})+),[0-9]{2}$`)

func parsePriceBRL(value string) (int, error) {
	value = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(value), "R$"))
	if !brlPattern.MatchString(value) {
		return 0, errors.New("informe o preço em reais, por exemplo R$ 38,00")
	}
	cents, err := strconv.Atoi(strings.NewReplacer(".", "", ",", "").Replace(value))
	if err != nil || cents < 0 || cents > 10000000 {
		return 0, errors.New("preço deve estar entre R$ 0,00 e R$ 100.000,00")
	}
	return cents, nil
}

func priceBRL(cents int) string {
	return fmt.Sprintf("R$ %d,%02d", cents/100, cents%100)
}
