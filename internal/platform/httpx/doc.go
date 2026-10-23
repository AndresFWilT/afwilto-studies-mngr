// Package httpx is the context-neutral HTTP kit shared by every bounded
// context of the service: the JSON envelope writers, request helpers, the
// middleware chain and the server composition. It knows no domain type;
// each context keeps its own error translation (Spec 18 D-10).
package httpx
