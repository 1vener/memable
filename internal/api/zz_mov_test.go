package api

import (
	"database/sql"
	"fmt"
	_ "modernc.org/sqlite"
	"os"
	"testing"
)

func TestDebugMov(t *testing.T) {
	dbPath := os.Getenv("LOCALAPPDATA") + `\memable\memable.db`
	dbh, err := sql.Open("sqlite", "file:"+dbPath+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	defer dbh.Close()
	rows, err := dbh.Query(`SELECT relative_path, format, video_codec, audio_codec, bit_rate, frame_rate FROM media WHERE lower(relative_path) LIKE '%.mov' LIMIT 15`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	n := 0
	for rows.Next() {
		var rel, f, vc, ac sql.NullString
		var br sql.NullInt64
		var fr sql.NullFloat64
		rows.Scan(&rel, &f, &vc, &ac, &br, &fr)
		fmt.Printf("%s | format=%s | video=%s | audio=%s | br=%v | fps=%v\n", rel.String, f.String, vc.String, ac.String, br.Int64, fr.Float64)
		n++
	}
	fmt.Printf("共 %d 条 mov\n", n)
	var total int
	dbh.QueryRow(`SELECT count(*) FROM media WHERE lower(relative_path) LIKE '%.mov'`).Scan(&total)
	fmt.Printf("mov 总数: %d\n", total)
}
