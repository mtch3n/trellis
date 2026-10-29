package pgtest

import (
	"context"
	"crypto/rand"
	"fmt"
	"net/url"
	"strings"
	"sync"
	"testing"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jmoiron/sqlx"
	"github.com/moby/moby/client"
	"github.com/testcontainers/testcontainers-go/modules/postgres"
)

const Image = "pgvector/pgvector:pg17"

type cluster struct {
	admin *sqlx.DB
	dsn   *url.URL
}

var shared = sync.OnceValues(func() (*cluster, error) {
	return start(context.Background())
})

func URL(tb testing.TB) string {
	tb.Helper()
	c, err := shared()
	if err != nil {
		tb.Fatalf("pgtest: %v", err)
	}
	return c.database(tb)
}

func Open(tb testing.TB) *sqlx.DB {
	tb.Helper()
	db, err := sqlx.Open("pgx", URL(tb))
	if err != nil {
		tb.Fatalf("pgtest: open: %v", err)
	}
	tb.Cleanup(func() { db.Close() })
	return db
}

func start(ctx context.Context) (c *cluster, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%v", r)
		}
		if err != nil {
			err = fmt.Errorf("start %s in Docker: %w; server tests need a running Docker daemon, reachable at DOCKER_HOST or the default socket", Image, err)
		}
	}()
	if err := pingDocker(ctx); err != nil {
		return nil, err
	}
	ctr, err := postgres.Run(ctx, Image,
		postgres.WithDatabase("trellis"),
		postgres.WithUsername("trellis"),
		postgres.WithPassword("trellis"),
		postgres.BasicWaitStrategies(),
	)
	if err != nil {
		return nil, err
	}
	raw, err := ctr.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		return nil, err
	}
	dsn, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	admin, err := sqlx.Open("pgx", raw)
	if err != nil {
		return nil, err
	}
	if err := admin.PingContext(ctx); err != nil {
		admin.Close()
		return nil, err
	}
	return &cluster{admin: admin, dsn: dsn}, nil
}

func pingDocker(ctx context.Context) error {
	cli, err := client.New(client.FromEnv)
	if err != nil {
		return err
	}
	defer cli.Close()
	_, err = cli.Ping(ctx, client.PingOptions{})
	return err
}

func (c *cluster) database(tb testing.TB) string {
	tb.Helper()
	name := "t_" + strings.ToLower(rand.Text())
	if _, err := c.admin.Exec(`CREATE DATABASE ` + name); err != nil {
		tb.Fatalf("pgtest: create database: %v", err)
	}
	tb.Cleanup(func() {
		if _, err := c.admin.Exec(`DROP DATABASE ` + name + ` WITH (FORCE)`); err != nil {
			tb.Errorf("pgtest: drop database %s: %v", name, err)
		}
	})
	u := *c.dsn
	u.Path = "/" + name
	return u.String()
}
