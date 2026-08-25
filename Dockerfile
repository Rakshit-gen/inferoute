FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/inferouted ./cmd/inferouted

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/inferouted /inferouted
EXPOSE 8081
ENTRYPOINT ["/inferouted"]
# /etc/secrets/config.json matches where Render's Secret Files feature
# mounts an uploaded file; docker-compose.yml mounts config.docker.json to
# the same path so both deployment paths share one default.
CMD ["-config", "/etc/secrets/config.json"]
