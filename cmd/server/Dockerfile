# Multi-stage build for the Gemeten Stad server. Build context is the repo root
# (see deploy/compose/compose.yaml) so the module and all packages are visible.
FROM golang:1.26 AS build
WORKDIR /src

# Cache module downloads separately from the source.
COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Static binary so it runs on the distroless/static base (no libc).
RUN CGO_ENABLED=0 go build -o /out/server ./cmd/server

FROM gcr.io/distroless/static:nonroot
COPY --from=build /out/server /server
EXPOSE 8080
ENTRYPOINT ["/server"]
