package main

import (
	"database/sql"
	"errors"
	"log"
	"os"

	"github.com/EurekaMXZ/assistant/internal/config"
	"github.com/golang-migrate/migrate/v4"
	pgxmigrate "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	cfg := config.Load()
	if err := cfg.ValidateMigration(); err != nil {
		log.Fatalf("invalid config: %v", err)
	}

	command := "up"
	if len(os.Args) > 1 {
		command = os.Args[1]
	}

	db, err := sql.Open("pgx", cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("open database: %v", err)
	}
	defer db.Close()

	driver, err := pgxmigrate.WithInstance(db, &pgxmigrate.Config{})
	if err != nil {
		log.Fatalf("create migrate driver: %v", err)
	}

	migrator, err := migrate.NewWithDatabaseInstance(cfg.MigrationsPath, "postgres", driver)
	if err != nil {
		log.Fatalf("create migrator: %v", err)
	}

	switch command {
	case "up":
		err = migrator.Up()
		if errors.Is(err, migrate.ErrNoChange) {
			log.Print("migrations are up to date")
			return
		}
		if err != nil {
			log.Fatalf("apply migrations: %v", err)
		}
		log.Print("migrations applied")
	case "down":
		err = migrator.Steps(-1)
		if errors.Is(err, migrate.ErrNoChange) {
			log.Print("no migrations to roll back")
			return
		}
		if err != nil {
			log.Fatalf("rollback migration: %v", err)
		}
		log.Print("rolled back one migration")
	case "version":
		version, dirty, err := migrator.Version()
		if err != nil {
			if errors.Is(err, migrate.ErrNilVersion) {
				log.Print("no migrations have been applied")
				return
			}
			log.Fatalf("get version: %v", err)
		}
		log.Printf("version=%d dirty=%t", version, dirty)
	default:
		log.Fatalf("unsupported command %q; expected one of: up, down, version", command)
	}
}
