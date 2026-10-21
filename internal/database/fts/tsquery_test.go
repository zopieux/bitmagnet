package fts_test

import (
	"database/sql"
	"testing"

	"github.com/bitmagnet-io/bitmagnet/internal/database/fts"
	_ "github.com/glebarez/go-sqlite"
	"github.com/stretchr/testify/assert"
)

//nolint:gosmopolitan
func TestAppQueryToTsquery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"1 word", "foo", "foo"},
		{"2 words", "foo Bar", "foo & bar"},
		{"3 words", "foo bar baz", "foo & bar & baz"},
		{
			"quotes, operators, parens, wildcards", "\"make me a \" . (sandwich | panini) !cheese mayo*",
			"make <-> me <-> a <-> (sandwich | panini) & ! cheese & mayo:*",
		},
		{"unmatched quotes", "\"make me a sandwich", "make <-> me <-> a <-> sandwich"},
		{
			"unmatched parens", "\"make me a \" . (sandwich | panini",
			"make <-> me <-> a <-> (sandwich | panini)",
		},
		{"Ukrainian", "зроби мені бутерброд", "zrobi & meni & buterbrod"},
		{"Chinese", "给我做一个三明治", "Gei <-> Wo <-> Zuo <-> Yi <-> Ge <-> San <-> Ming <-> Zhi"},
		{"Arabic", "اصنع لي شطيرة", "'Sn`' & ly & 'shTyr@'"},
		{"Arabic (quoted)", "\"اصنع لي شطيرة\"", "'Sn`' <-> ly <-> 'shTyr@'"},
		{"ampersand prefix", "&eacute;", "eacute"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, fts.AppQueryToTsquery(tt.input))
		})
	}
}

func TestAppQueryToFtsQuery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		input string
		want  string
	}{
		{"empty", "", ""},
		{"1 word", "foo", "foo"},
		{"2 words", "foo Bar", "foo AND bar"},
		{"3 words", "foo bar baz", "foo AND bar AND baz"},
		{
			"quotes, operators, parens, wildcards", "\"make me a \" . (sandwich | panini) !cheese mayo*",
			`"make me a" AND (sandwich OR panini) NOT cheese AND mayo*`,
		},
		{"or", "foo | bar", "foo OR bar"},
		{"negation", "foo !bar", "foo NOT bar"},
		{"wildcard", "mayo*", "mayo*"},
		{"quoted", "\"foo bar\"", `"foo bar"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, fts.AppQueryToFtsQuery(tt.input))
		})
	}
}

func TestAppQueryToFtsQueryExecutes(t *testing.T) {
	t.Parallel()

	db, err := sql.Open("sqlite", ":memory:")
	assert.NoError(t, err)
	defer db.Close()

	_, err = db.Exec(`CREATE VIRTUAL TABLE fts USING fts5(name)`)
	assert.NoError(t, err)
	_, err = db.Exec(`INSERT INTO fts(rowid, name) VALUES
		(1, 'the quick brown fox'),
		(2, 'lazy dog cheese'),
		(3, 'sandwich panini mayo')`)
	assert.NoError(t, err)

	tests := []struct {
		input string
		want  []int64
	}{
		{"quick fox", []int64{1}},
		{"quick | dog", []int64{1, 2}},
		{`"quick brown"`, []int64{1}},
		{"sandwich !cheese", []int64{3}},
		{"mayo*", []int64{3}},
		{`"sandwich panini"`, []int64{3}},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			q := fts.AppQueryToFtsQuery(tt.input)
			rows, err := db.Query(`SELECT rowid FROM fts WHERE fts MATCH ? ORDER BY rowid`, q)
			if !assert.NoError(t, err) {
				return
			}
			defer rows.Close()
			var got []int64
			for rows.Next() {
				var id int64
				assert.NoError(t, rows.Scan(&id))
				got = append(got, id)
			}
			assert.Equal(t, tt.want, got)
		})
	}
}
