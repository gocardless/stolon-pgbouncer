package pgbouncer

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"fmt"
	"html/template"
	"io/ioutil"
	"os"
	"regexp"

	"github.com/jackc/pgx"
	"github.com/pkg/errors"
)

type PgBouncer struct {
	ConfigFile         string
	ConfigTemplateFile string // template that can be rendered with Host value
	Executor           executor
}

// Config generates a key value map of config parameters from the PgBouncer config
// template file
func (b *PgBouncer) Config() (map[string]string, error) {
	config := make(map[string]string)
	configFile, err := os.Open(b.ConfigTemplateFile)

	if err != nil {
		return nil, errors.Wrap(err, "failed to read PgBouncer config template file")
	}

	defer configFile.Close()

	r, _ := regexp.Compile("^(\\S+)\\s*\\=\\s*(\\S+)$")
	scanner := bufio.NewScanner(configFile)

	for scanner.Scan() {
		line := scanner.Text()
		if result := r.FindStringSubmatch(line); result != nil {
			config[result[1]] = result[2]
		}
	}

	return config, nil
}

// GenerateConfig writes new configuration to PgBouncer.ConfigFile
func (b *PgBouncer) GenerateConfig(host string) error {
	var configBuffer bytes.Buffer
	template, err := b.createTemplate()

	if err != nil {
		return err
	}

	err = template.Execute(&configBuffer, struct{ Host string }{host})

	if err != nil {
		return errors.Wrap(err, "failed to render PgBouncer config")
	}

	return ioutil.WriteFile(b.ConfigFile, configBuffer.Bytes(), 0644)
}

func (b *PgBouncer) createTemplate() (*template.Template, error) {
	configTemplate, err := ioutil.ReadFile(b.ConfigTemplateFile)

	if err != nil {
		return nil, errors.Wrap(err, "failed to read PgBouncer config template file")
	}

	return template.Must(template.New("PgBouncerConfig").Parse(string(configTemplate))), err
}

type Database struct {
	Name, Host, Port   string
	CurrentConnections int64
}

// ShowDatabases extracts information from the SHOW DATABASES PgBouncer command, selecting
// columns about database host details. This is quite cumbersome to write, due to the
// inability to query select fields for database information, and the lack of guarantees
// about the ordering of the columns returned from the command.
func (b *PgBouncer) ShowDatabases(ctx context.Context) ([]Database, error) {
	databases := make([]Database, 0)
	rows, err := b.Executor.Query(ctx, `SHOW DATABASES;`)

	if err != nil {
		return databases, err
	}

	rows.Next()
	defer rows.Close()

	fields := rows.FieldDescriptions()
	columnPointers := make([]interface{}, len(fields))

	indexOfColumn := func(c string) int {
		for idx, field := range fields {
			if field.Name == c {
				return idx
			}
		}

		return -1
	}

	var name, host, port, null sql.NullString
	var currentConnections sql.NullInt64

	for idx := range columnPointers {
		columnPointers[idx] = &null
	}

	columnPointers[indexOfColumn("name")] = &name
	columnPointers[indexOfColumn("host")] = &host
	columnPointers[indexOfColumn("port")] = &port
	columnPointers[indexOfColumn("current_connections")] = &currentConnections

	for {
		err := rows.Scan(columnPointers...)

		if err != nil {
			return databases, err
		}

		databases = append(databases, Database{
			name.String, host.String, port.String, currentConnections.Int64,
		})

		if !rows.Next() {
			break
		}
	}

	return databases, rows.Err()
}

// These error codes are returned whenever PgBouncer is asked to PAUSE/RESUME, but is
// already in the given state.
const PoolerError = "08P01"
const AlreadyPausedError = "already suspended/paused"
const AlreadyResumedError = "pooler is not paused/suspended"

// Pause causes PgBouncer to buffer incoming queries while waiting for those currently
// processing to finish executing. The supplied timeout is applied to the Postgres
// connection.
func (b *PgBouncer) Pause(ctx context.Context) error {
	if err := b.Executor.Execute(ctx, `PAUSE;`); err != nil {
		if err, ok := err.(pgx.PgError); ok {
			if string(err.Code) == PoolerError && err.Message == AlreadyPausedError {
				return nil
			}
		}

		return err
	}

	return nil
}

// Resume will remove any applied pauses to PgBouncer
func (b *PgBouncer) Resume(ctx context.Context) error {
	if err := b.Executor.Execute(ctx, `RESUME;`); err != nil {
		if err, ok := err.(pgx.PgError); ok {
			if string(err.Code) == PoolerError && err.Message == AlreadyResumedError {
				return nil
			}
		}

		return err
	}

	return nil
}

// Disable causes PgBouncer to reject all new client connections on the given databases.
// If no databases are supplied then this operation will apply to all PgBouncer databases.
func (b *PgBouncer) Disable(ctx context.Context, databases ...string) error {
	if len(databases) == 0 {
		dbs, err := b.ShowDatabases(ctx)
		if err != nil {
			return err
		}

		for _, db := range dbs {
			if db.Name != "pgbouncer" {
				databases = append(databases, db.Name)
			}
		}
	}

	for _, database := range databases {
		if err := b.Executor.Execute(ctx, fmt.Sprintf(`DISABLE %s;`, database)); err != nil {
			return err
		}
	}

	return nil
}

// Reload will cause PgBouncer to reload configuration and live apply setting changes
func (b *PgBouncer) Reload(ctx context.Context) error {
	return b.Executor.Execute(ctx, `RELOAD;`)
}

// Connect runs the most basic of commands (SHOW VERSION) against PgBouncer to ensure the
// connection is alive.
func (b *PgBouncer) Connect(ctx context.Context) error {
	return b.Executor.Execute(ctx, `SHOW VERSION;`)
}
