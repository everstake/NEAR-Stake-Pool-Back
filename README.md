## Deploy & run
At first, need to prepare a postgres db. The migration will run automatically after the application starts.
1. copy `.env.example`  to `.env` 
 ```
cp .env.example .env
```
2. fill `.env` file with your settings
> NODE - address of NEAR RPC node

> STAKE_POOL - stake pool contract address
3. build and run application
```
go build ./cmd/lido && ./lido
```
## Tests
```
go test ./...
```