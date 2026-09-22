.PHONY: up down backend frontend install build help

# Start backend + frontend together (Ctrl+C stops both)
up:
	@echo "→ backend  http://localhost:8081"
	@echo "→ frontend http://localhost:5173"
	@trap 'kill 0' INT TERM EXIT; \
		(cd backend && go run .) & \
		(cd frontend && npm run dev) & \
		wait

backend:
	cd backend && go run .

frontend:
	cd frontend && npm run dev

install:
	cd frontend && npm install

build:
	cd frontend && npm run build

# Production: built UI served by backend on :8081
serve: build
	cd backend && go run . -frontend ../frontend/dist

help:
	@echo "make up       — backend + frontend (dev)"
	@echo "make backend  — только Go API (:8081)"
	@echo "make frontend — только Vite (:5173)"
	@echo "make install  — npm install"
	@echo "make build    — собрать frontend"
	@echo "make serve    — production (бинарник + dist)"
