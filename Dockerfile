# Backend (pokerd). Pensado para el modo nube (configs/api.yaml): no necesita GPU.
FROM golang:1.27-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/pokerd ./cmd/pokerd

FROM alpine:3.22
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 poker
WORKDIR /app
COPY --from=build /out/pokerd /usr/local/bin/pokerd
COPY configs ./configs
COPY recordings/sample.jsonl ./recordings/sample.jsonl
RUN mkdir -p /app/recordings && chown -R poker /app/recordings
USER poker
EXPOSE 8090
# Las claves llegan como variables de entorno (env_file en compose); nunca se copian en la imagen.
ENTRYPOINT ["pokerd"]
CMD ["serve", "--config", "configs/api.yaml", "--addr", "0.0.0.0:8090"]
