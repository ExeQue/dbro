package db

import (
	"regexp"
	"strings"
)

var typeArgs = regexp.MustCompile(`\(([^)]*)\)`)

// MapTableDef translates a table definition between dialects (mysql, postgres, sqlite).
func MapTableDef(def TableDef, from, to string) TableDef {
	out := def
	out.Columns = make([]Column, len(def.Columns))
	indexed := map[string]bool{}
	for _, ix := range def.Indexes {
		for _, c := range ix.Columns {
			indexed[c] = true
		}
	}
	for _, fk := range def.ForeignKeys {
		for _, c := range fk.Columns {
			indexed[c] = true
		}
	}
	for i, col := range def.Columns {
		col.Type = MapType(col.Type, from, to)
		if to == "mysql" && indexed[col.Name] && (col.Type == "text" || col.Type == "longtext") {
			col.Type = "varchar(255)"
		}
		if from != to {
			col.Default = mapDefault(col.Default)
		}
		if to == "sqlite" && col.AutoIncrement {
			col.Type = "INTEGER"
		}
		out.Columns[i] = col
	}
	if from != to {
		out.Create = ""
		// index names are per-schema in postgres; keep them unique by prefixing the table
		out.Indexes = make([]Index, len(def.Indexes))
		for i, ix := range def.Indexes {
			if !ix.Primary && to == "postgres" && !strings.HasPrefix(ix.Name, def.Name) {
				ix.Name = def.Name + "_" + ix.Name
			}
			out.Indexes[i] = ix
		}
	}
	if to == "sqlite" {
		// SQLite autoincrement only works on a single INTEGER PRIMARY KEY.
		if pk := out.PrimaryKey(); len(pk) != 1 {
			for i := range out.Columns {
				out.Columns[i].AutoIncrement = false
			}
		}
	}
	return out
}

func mapDefault(d *string) *string {
	if d == nil {
		return nil
	}
	v := strings.TrimSpace(*d)
	up := strings.ToUpper(v)
	switch {
	case numeric.MatchString(v), up == "NULL":
		return d
	case strings.HasPrefix(v, "'"):
		// strip postgres casts: 'x'::character varying
		if i := strings.LastIndex(v, "'::"); i > 0 {
			v = v[:i+1]
		}
		return &v
	case strings.HasPrefix(up, "CURRENT_TIMESTAMP"), up == "NOW()", up == "CURRENT_DATE":
		s := "CURRENT_TIMESTAMP"
		if up == "CURRENT_DATE" {
			s = "CURRENT_DATE"
		}
		return &s
	case up == "TRUE" || up == "FALSE":
		s := map[string]string{"TRUE": "1", "FALSE": "0"}[up]
		return &s
	}
	return nil // expressions do not translate
}

// MapType translates one column type.
func MapType(t, from, to string) string {
	if from == to {
		return t
	}
	lt := strings.ToLower(strings.TrimSpace(t))
	unsigned := strings.Contains(lt, "unsigned")
	lt = strings.TrimSpace(strings.ReplaceAll(strings.ReplaceAll(lt, "unsigned", ""), "zerofill", ""))
	args := ""
	if m := typeArgs.FindStringSubmatch(lt); m != nil {
		args = m[1]
	}
	base := strings.TrimSpace(typeArgs.ReplaceAllString(lt, ""))
	withArgs := func(name string) string {
		if args == "" {
			return name
		}
		return name + "(" + args + ")"
	}

	kind := classify(base, args)
	switch to {
	case "sqlite":
		switch kind {
		case "int", "smallint", "bigint", "bool":
			return "INTEGER"
		case "decimal":
			return "NUMERIC"
		case "float", "double":
			return "REAL"
		case "blob":
			return "BLOB"
		}
		return "TEXT"
	case "postgres":
		switch kind {
		case "int":
			if unsigned {
				return "bigint"
			}
			return "integer"
		case "smallint":
			return "smallint"
		case "bigint":
			if unsigned {
				return "numeric(20)"
			}
			return "bigint"
		case "bool":
			return "boolean"
		case "decimal":
			return withArgs("numeric")
		case "float":
			return "real"
		case "double":
			return "double precision"
		case "varchar":
			return withArgs("varchar")
		case "char":
			return withArgs("char")
		case "datetime":
			return "timestamp"
		case "timestamptz":
			return "timestamptz"
		case "date":
			return "date"
		case "time":
			return "time"
		case "json":
			return "jsonb"
		case "blob":
			return "bytea"
		case "uuid":
			return "uuid"
		}
		return "text"
	case "mysql":
		switch kind {
		case "int":
			return "int"
		case "smallint":
			return "smallint"
		case "bigint":
			return "bigint"
		case "bool":
			return "tinyint(1)"
		case "decimal":
			if args == "" {
				return "decimal(65,10)"
			}
			return withArgs("decimal")
		case "float":
			return "float"
		case "double":
			return "double"
		case "varchar":
			if args == "" {
				return "varchar(255)"
			}
			return withArgs("varchar")
		case "char":
			return withArgs("char")
		case "datetime", "timestamptz":
			return "datetime(6)"
		case "date":
			return "date"
		case "time":
			return "time"
		case "json":
			return "json"
		case "blob":
			return "longblob"
		case "uuid":
			return "char(36)"
		}
		return "longtext"
	}
	return t
}

func classify(base, args string) string {
	switch {
	case base == "tinyint" && args == "1", base == "boolean", base == "bool", base == "bit" && (args == "" || args == "1"):
		return "bool"
	case base == "tinyint", base == "smallint", base == "int2", base == "year":
		return "smallint"
	case base == "bigint", base == "int8", base == "bigserial", base == "serial8":
		return "bigint"
	case strings.Contains(base, "int"), base == "serial", base == "smallserial":
		return "int"
	case base == "decimal", base == "numeric", base == "dec", base == "money":
		return "decimal"
	case base == "float", base == "real", base == "float4":
		return "float"
	case base == "double", base == "double precision", base == "float8":
		return "double"
	case base == "varchar", base == "character varying", base == "nvarchar", base == "varchar2":
		return "varchar"
	case base == "char", base == "character", base == "nchar", base == "bpchar":
		return "char"
	case base == "datetime", base == "timestamp", base == "timestamp without time zone", strings.HasPrefix(base, "timestamp") && !strings.Contains(base, "with time zone"):
		return "datetime"
	case base == "timestamptz", base == "timestamp with time zone":
		return "timestamptz"
	case base == "date":
		return "date"
	case strings.HasPrefix(base, "time"):
		return "time"
	case base == "json", base == "jsonb":
		return "json"
	case strings.Contains(base, "blob"), strings.Contains(base, "binary"), base == "bytea":
		return "blob"
	case base == "uuid":
		return "uuid"
	}
	return "text"
}
