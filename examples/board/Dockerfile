# Build stage: compile one static binary. Templates and static files are
# embedded with go:embed, so the binary is everything the image needs.
FROM golang:1.27 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/board .

# Runtime stage: a minimal image with no shell, running as a non-root user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/board /board
EXPOSE 8000
ENTRYPOINT ["/board"]
