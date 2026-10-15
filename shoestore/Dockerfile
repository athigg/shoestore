FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/shoestore .

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -H app
WORKDIR /app
COPY --from=build /out/shoestore .
COPY templates ./templates
COPY static ./static
RUN mkdir -p uploads && chown -R app:app /app
USER app
ENV GIN_MODE=release PORT=8080
EXPOSE 8080
CMD ["./shoestore"]
