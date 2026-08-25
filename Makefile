.PHONY: test test-docker build run-docker-server stop-docker-server

test:
	go test -v -count=1 ./tests/...

build:
	docker build -t locali-e2e-platform .

test-docker:
	docker build -t locali-e2e-platform . && docker run --rm locali-e2e-platform go test -v -count=1 ./tests/...

run-docker-server:
	docker compose up --build -d

stop-docker-server:
	docker compose down
