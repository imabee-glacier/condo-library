// Package condo is a direct-HTTP SDK for the Superlógica Área do Condômino
// resident portal (Dinâmica Condomínios SC).
//
// It is not a browser-automation wrapper. At runtime it makes ordinary HTTP
// requests to the portal's JSON API (see README.md for the traced endpoints)
// and returns JSON-serialisable results. PDFs are only fetched when the
// caller explicitly asks for a document.
//
// Authentication is supplied by the caller at runtime and held in memory
// only. The SDK never writes a username, password, token, or session cookie
// to disk, to a file, to an environment file, to logs, or to errors.
// Muse's Secure Vault does NOT hand a password back to an SDK; for
// autonomous use this SDK uses its own encrypted credential store,
// enrolled once via setup-auth (see README.md).
//
// Every read is data-first and JSON-first. Money and meter-style quantities
// are preserved as decimal strings, never as binary floating point, and a
// missing field means "not reported", not zero. See README.md for the full
// tool catalog, safety tiers, and limitations.
//
// Version: 0.1.0
package condo
