package pgbouncer

import (
	"context"
	"strconv"

	"github.com/jackc/pgx"
	"github.com/jackc/pgx/pgtype"
	"github.com/pkg/errors"
)

type executor interface {
	Query(context.Context, string, ...interface{}) (*pgx.Rows, error)
	Execute(context.Context, string, ...interface{}) error
}

type AuthorizedExecutor struct {
	User, Password, Database, SocketDir, Port string
}

func (e AuthorizedExecutor) Query(ctx context.Context, query string, params ...interface{}) (*pgx.Rows, error) {
	conn, err := e.Connection()

	if err != nil {
		return nil, err
	}

	return conn.QueryEx(ctx, query, &pgx.QueryExOptions{SimpleProtocol: true}, params...)
}

func (e AuthorizedExecutor) Execute(ctx context.Context, query string, params ...interface{}) error {
	conn, err := e.Connection()

	if err != nil {
		return err
	}

	_, err = conn.ExecEx(ctx, query, &pgx.QueryExOptions{SimpleProtocol: true}, params...)
	return err
}

func (e AuthorizedExecutor) Connection() (*pgx.Conn, error) {
	port, err := strconv.Atoi(e.Port)
	if err != nil {
		return nil, errors.Wrap(err, "failed to parse valid port number")
	}

	return pgx.Connect(
		pgx.ConnConfig{
			Database:      e.Database,
			User:          e.User,
			Password:      e.Password,
			Host:          e.SocketDir,
			Port:          uint16(port),
			RuntimeParams: map[string]string{"client_encoding": "UTF8"},
			// We need to use SimpleProtocol in order to communicate with PgBouncer
			PreferSimpleProtocol: true,
			CustomConnInfo: func(_ *pgx.Conn) (*pgtype.ConnInfo, error) {
				connInfo := pgtype.NewConnInfo()
				connInfo.InitializeDataTypes(map[string]pgtype.OID{
					"int4":    pgtype.Int4OID,
					"name":    pgtype.NameOID,
					"oid":     pgtype.OIDOID,
					"text":    pgtype.TextOID,
					"varchar": pgtype.VarcharOID,
				})

				return connInfo, nil
			},
		},
	)
}
