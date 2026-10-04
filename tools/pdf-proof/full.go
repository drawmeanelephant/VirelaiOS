//go:build virelai

package main

import "virelai/pdf"

func render(s pdf.Source, index int, arena []byte, ledger *pdf.Ledger) (pdf.Page, pdf.Stats, pdf.Failure) {
	return pdf.Render(s, index, arena, ledger)
}
func codeName(c pdf.Code) string { return (pdf.Failure{Code: c}).Error() }
