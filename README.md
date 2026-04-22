prompt 2 migration command

`migrate -path internal/db/migrations -database "postgresql://ssp:ssp@localhost:5433/ssp_db?sslmode=disable" up
`

`make migrate-up`


to run docker compose files for prompt 3.3

`docker compose up --build`