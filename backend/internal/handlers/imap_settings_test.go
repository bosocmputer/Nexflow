package handlers

import (
	"testing"

	"nexflow/internal/models"
)

func TestNormalizeIMAPUpsertRemovesGmailAppPasswordSeparators(t *testing.T) {
	in := models.IMAPAccountUpsert{
		Host:     " IMAP.GMAIL.COM ",
		Username: " boss@example.com ",
		Password: "qzqq vwqb-zydo\tdtsi",
		Mailbox:  " INBOX ",
	}

	normalizeIMAPUpsert(&in)

	if in.Host != "imap.gmail.com" {
		t.Fatalf("Host = %q, want imap.gmail.com", in.Host)
	}
	if in.Username != "boss@example.com" {
		t.Fatalf("Username = %q, want trimmed email", in.Username)
	}
	if in.Password != "qzqqvwqbzydodtsi" {
		t.Fatalf("Password = %q, want normalized app password", in.Password)
	}
	if got := validateIMAPUpsert(in, true); got != "" {
		t.Fatalf("validateIMAPUpsert() = %q, want no error", got)
	}
}

func TestValidateIMAPUpsertRejectsShortGmailAppPassword(t *testing.T) {
	in := models.IMAPAccountUpsert{
		Host:     "imap.gmail.com",
		Password: "qzqq vwqb zydo dts",
	}

	normalizeIMAPUpsert(&in)

	if in.Password != "qzqqvwqbzydodts" {
		t.Fatalf("Password = %q, want normalized short app password", in.Password)
	}
	if got := validateIMAPUpsert(in, true); got == "" {
		t.Fatal("validateIMAPUpsert() = empty, want error for short Gmail app password")
	}
}
