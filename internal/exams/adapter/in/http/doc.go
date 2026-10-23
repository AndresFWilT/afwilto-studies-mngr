// Package http is the Exams bounded context's inbound HTTP adapter. It owns
// its own translateError, since domain error types are not shared between
// contexts (Spec 18 D-10); the envelope, middleware and server composition
// come from internal/platform/httpx.
package http
