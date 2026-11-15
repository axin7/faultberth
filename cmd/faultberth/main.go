package main

import (
	"context"
	"crypto/rand"
	"errors"
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/axin7/faultberth/internal/admin"
	"github.com/axin7/faultberth/internal/ingest"
	"github.com/axin7/faultberth/internal/store"
)

func main() {
	dbPath := flag.String("db", "faultberth.sqlite", "SQLite database path")
	listen := flag.String("listen", "127.0.0.1:18100", "loopback listen address")
	importPath := flag.String("import-bugsink", "", "import a Bugsink SQLite snapshot and exit")
	adminUser := flag.String("create-admin", "", "create an administrator and exit")
	passwordFile := flag.String("password-file", "", "private file containing the administrator password")
	project := flag.String("create-project", "", "create a project and exit")
	flag.Parse()
	if err := run(*dbPath, *listen, *importPath, *adminUser, *passwordFile, *project); err != nil {
		log.Fatal(err)
	}
}

func run(dbPath, listen, importPath, adminUser, passwordFile, project string) error {
	ops := 0
	for _, value := range []string{importPath, adminUser, project} {
		if value != "" {
			ops++
		}
	}
	if ops > 1 || (passwordFile != "") != (adminUser != "") {
		return errors.New("choose one operation; creating an administrator requires a password file")
	}
	if importPath != "" {
		if err := validateImportPaths(dbPath, importPath); err != nil {
			return err
		}
	}
	if err := ensurePrivateFile(dbPath); err != nil {
		return err
	}
	s, err := store.Open(dbPath)
	if err != nil {
		return err
	}
	defer s.Close()
	if importPath != "" {
		counts, err := s.ImportBugsink(context.Background(), importPath)
		if err != nil {
			return err
		}
		fmt.Printf("imported projects=%d users=%d issues=%d events=%d releases=%d\n",
			counts.Projects, counts.Users, counts.Issues, counts.Events, counts.Releases)
		return nil
	}
	if adminUser != "" {
		password, err := readPassword(passwordFile)
		if err != nil {
			return err
		}
		return s.CreateAdmin(context.Background(), adminUser, password)
	}
	if project != "" {
		id, key, err := s.CreateProject(context.Background(), project)
		if err == nil {
			fmt.Printf("project_id=%d sentry_key=%s\n", id, key)
		}
		return err
	}
	if err := requireLoopback(listen); err != nil {
		return err
	}
	secret := make([]byte, 32)
	if _, err := rand.Read(secret); err != nil {
		return err
	}
	mux := http.NewServeMux()
	mux.Handle("/api/", ingest.Handler{Store: s})
	mux.Handle("/", admin.Handler{Store: s, Secret: secret})
	mux.HandleFunc("/health/ready", func(w http.ResponseWriter, r *http.Request) {
		if err := s.Ready(r.Context()); err != nil {
			http.Error(w, "database unavailable", http.StatusServiceUnavailable)
			return
		}
		_, _ = w.Write([]byte("OK"))
	})
	server := &http.Server{Addr: listen, Handler: mux, ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdown)
	}()
	log.Printf("listening on %s", listen)
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func validateImportPaths(destination, source string) error {
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return err
	}
	destinationInfo, err := os.Stat(destination)
	if err == nil && os.SameFile(sourceInfo, destinationInfo) {
		return errors.New("source and destination databases must be different files")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	sourcePath, err := filepath.Abs(source)
	if err != nil {
		return err
	}
	destinationPath, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if sourcePath == destinationPath {
		return errors.New("source and destination databases must be different files")
	}
	return nil
}

func readPassword(path string) (string, error) {
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 || info.Size() > 4096 {
		return "", errors.New("password file must be private, regular and at most 4096 bytes")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r"), nil
}

func ensurePrivateFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if errors.Is(err, os.ErrExist) {
		info, err := os.Lstat(path)
		if err != nil {
			return err
		}
		if !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
			return errors.New("database must be a private regular file")
		}
		return nil
	}
	if err != nil {
		return err
	}
	return f.Close()
}

func requireLoopback(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return errors.New("listener must use a loopback IP address")
	}
	return nil
}
