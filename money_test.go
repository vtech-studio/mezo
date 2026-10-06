package main

import "testing"

func TestPricesInReaisPreserveCents(t *testing.T) {
	for _, test := range []struct {
		input string
		cents int
	}{
		{"R$ 38,00", 3800}, {"R$\u00a00,01", 1}, {"12,34", 1234},
		{"R$ 1.234,56", 123456}, {"100.000,00", 10000000}, {"0,00", 0},
	} {
		value, err := parsePriceBRL(test.input)
		if err != nil || value != test.cents {
			t.Fatalf("%q: esperado %d centavos, recebeu %d (%v)", test.input, test.cents, value, err)
		}
	}
	for _, input := range []string{"", "3800", "38.00", "38,0", "1.23,45", "R$ -38,00", "100.000,01", "R$ 38,00 lixo"} {
		if _, err := parsePriceBRL(input); err == nil {
			t.Errorf("aceitou preço inválido/ambíguo: %q", input)
		}
	}
}
