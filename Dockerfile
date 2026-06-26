# --- Estágio de Compilação ---
FROM golang:1.26-alpine AS builder

WORKDIR /src

# Copia dependências primeiro para aproveitar o cache do Docker
COPY go.mod ./
RUN go mod download

# Copia o restante do código fonte
COPY cmd/ ./cmd/
COPY internal/ ./internal/

# Compila o executável de forma estática e otimizada (sem símbolos de depuração para diminuir o tamanho)
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /app/monitor ./cmd/monitor


# --- Estágio de Execução ---
FROM alpine:latest

# Instala CA Certificates para HTTPS seguro e TZData para sincronização de fuso horário
RUN apk --no-cache add ca-certificates tzdata

WORKDIR /app


# Copia o binário compilado
COPY --from=builder /app/monitor /app/monitor

# Copia os assets da Landing Page
COPY web/ /app/web/

# Mapeia a porta padrão exposta pelo servidor web
EXPOSE 8080

# Inicia a aplicação
ENTRYPOINT ["/app/monitor"]
