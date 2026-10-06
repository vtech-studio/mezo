package main

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"strconv"
	"strings"
	"time"
	_ "time/tzdata"

	"github.com/labstack/echo/v4"
)

type Settlement struct {
	Table   int    `json:"table"`
	Method  string `json:"method"`
	Total   int64  `json:"total"`
	Staff   string `json:"staff"`
	Created int64  `json:"created"`
}

func (a *App) receipts(c echo.Context) error {
	x, err := a.staffAccess(c, false, "manager")
	if err != nil {
		return err
	}
	defer x.Release()
	zone, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		return err
	}
	date := c.QueryParam("date")
	if date == "" {
		date = time.Now().In(zone).Format("2006-01-02")
	}
	if _, err = time.Parse("2006-01-02", date); err != nil {
		return echo.NewHTTPError(400, "Data inválida")
	}
	rows, err := x.Pool.Query(c.Request().Context(), `SELECT t.table_number,v.method,v.total,u.name,(extract(epoch FROM v.created_at)*1000)::bigint FROM settlements v JOIN table_sessions t ON t.id=v.session_id JOIN staff u ON u.id=v.closed_by WHERE (v.created_at AT TIME ZONE 'America/Sao_Paulo')::date=$1::date ORDER BY v.created_at DESC`, date)
	if err != nil {
		return err
	}
	defer rows.Close()
	records := []Settlement{}
	methods := map[string]int64{}
	var total int64
	for rows.Next() {
		var v Settlement
		if err = rows.Scan(&v.Table, &v.Method, &v.Total, &v.Staff, &v.Created); err != nil {
			return err
		}
		total += v.Total
		methods[v.Method] += v.Total
		records = append(records, v)
	}
	if err = rows.Err(); err != nil {
		return err
	}
	if c.QueryParam("format") == "csv" {
		var buf bytes.Buffer
		buf.WriteString("\xef\xbb\xbf")
		writer := csv.NewWriter(&buf)
		writer.Comma = ';'
		_ = writer.Write([]string{"Mesa", "Forma presencial", "Total (R$)", "Registrado por", "Horário (São Paulo)"})
		for _, v := range records {
			_ = writer.Write([]string{strconv.Itoa(v.Table), v.Method, fmt.Sprintf("%d,%02d", v.Total/100, v.Total%100), csvSafe(v.Staff), time.UnixMilli(v.Created).In(zone).Format("02/01/2006 15:04:05")})
		}
		writer.Flush()
		if err = writer.Error(); err != nil {
			return err
		}
		c.Response().Header().Set("Content-Disposition", "attachment; filename=recebimentos-"+date+".csv")
		return c.Blob(200, "text/csv; charset=utf-8", buf.Bytes())
	}
	return c.JSON(200, map[string]any{"date": date, "total": total, "count": len(records), "methods": methods, "records": records})
}
func csvSafe(text string) string {
	trimmed := strings.TrimSpace(text)
	if trimmed != "" && strings.ContainsAny(trimmed[:1], "=+-@") {
		return "'" + text
	}
	return text
}
