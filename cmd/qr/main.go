// Generate a table QR from its already-published, absolute HTTPS URL.
package main

import (
	"flag"
	"fmt"
	"html"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/skip2/go-qrcode"
)

func main() {
	target := flag.String("url", "", "URL HTTPS da mesa publicada")
	table := flag.Int("table", 8, "número da mesa")
	out := flag.String("out", "qr-mezo-mesa-08", "prefixo dos arquivos PNG/SVG")
	flag.Parse()
	u, err := url.Parse(*target)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || *table < 1 || *table > 500 {
		fmt.Fprintln(os.Stderr, "Informe uma URL HTTPS publicada e uma mesa entre 1 e 500.")
		os.Exit(1)
	}
	q, err := qrcode.New(*target, qrcode.High)
	if err != nil {
		panic(err)
	}
	if err = os.MkdirAll(filepath.Dir(*out), 0755); err != nil {
		panic(err)
	}
	if err = q.WriteFile(1024, *out+".png"); err != nil {
		panic(err)
	}
	bitmap := q.Bitmap()
	n := len(bitmap)
	var cells strings.Builder
	for y, row := range bitmap {
		for x, on := range row {
			if on {
				fmt.Fprintf(&cells, "<rect x='%d' y='%d' width='1' height='1'/>", x, y)
			}
		}
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" width="800" height="1000" viewBox="0 0 800 1000"><title>Mezô · Mesa %02d</title><desc>%s</desc><rect width="800" height="1000" rx="30" fill="#f7f5ef"/><text x="400" y="112" text-anchor="middle" fill="#a14f35" font-family="Georgia,serif" font-weight="bold" font-size="76">mezô</text><text x="400" y="180" text-anchor="middle" fill="#344239" font-family="Arial,sans-serif" font-size="30">Mesa %02d</text><svg x="80" y="220" width="640" height="640" viewBox="0 0 %d %d" shape-rendering="crispEdges"><rect width="%d" height="%d" fill="white"/><g fill="black">%s</g></svg><text x="400" y="926" text-anchor="middle" fill="#344239" font-family="Arial,sans-serif" font-size="26">Escaneie. Seu lugar já está aqui.</text></svg>`, *table, html.EscapeString(*target), *table, n, n, n, n, cells.String())
	if err = os.WriteFile(*out+".svg", []byte(svg), 0644); err != nil {
		panic(err)
	}
	fmt.Printf("QR da mesa %02d gerado em PNG e SVG.\n", *table)
}
