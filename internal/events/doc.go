// Package events ingests discrete events from pull-based sources
// (file_tail, docker_logs), stores them in the shared SQLite
// alongside samples, and exposes a Store for the web layer to
// query.
package events
