build:
	go build -o bin/server ./cmd/server

proto:
	protoc --go_out=. --go-grpc_out=. proto/localcloud.proto

run:
	docker compose up --build

migrate:
	# run migration inside db container
	docker compose exec db psql -U ${DB_USER} -d ${DB_NAME} -f /migrations/0001_init.sql