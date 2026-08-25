FROM golang:1.25 AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -o /out/inferouted ./cmd/inferouted

FROM gcr.io/distroless/static-debian12
COPY --from=build /out/inferouted /inferouted
ENTRYPOINT ["/inferouted"]
CMD ["-config", "/etc/inferoute/config.json"]
