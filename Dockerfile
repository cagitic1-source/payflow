# Сборка: полный образ Go с компилятором. В итоговый образ не попадает.
FROM golang:1.27.2-alpine AS build

WORKDIR /src

# Сначала только описание зависимостей: этот слой пересобирается, только
# когда меняются go.mod или go.sum, а не при каждой правке кода.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# Версия приходит снаружи: docker build --build-arg VERSION=... .
ARG VERSION=dev

# CGO_ENABLED=0 - статический бинарник без libc: в образе static её нет.
# -trimpath убирает из бинарника пути к исходникам на машине сборmakeки.
# -s -w убирают таблицу символов и отладочную информацию: бинарник меньше,
# а стектрейсы паник остаются.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/payment-api ./cmd/payment-api

# Запуск: только бинарник, CA-сертификаты и tzdata. Ни shell, ни менеджера пакетов.
FROM gcr.io/distroless/static-debian13:nonroot

COPY --from=build /out/payment-api /payment-api

# Документация портов: API и служебный сервер с /metrics.
EXPOSE 8080 8081

# Образ nonroot уже запускает от uid 65532. Повторяем числом, а не именем:
# Kubernetes с runAsNonRoot не может проверить пользователя по имени
# и не запустит под.
USER 65532:65532

# Exec-форма: payment-api становится PID 1 и сам получает SIGTERM.
ENTRYPOINT ["/payment-api"]