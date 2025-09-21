# GitChat

A simple chat server built with Go and Gin framework.

## Features

- RESTful API for chat messages
- CORS support for web clients
- Health check endpoint
- In-memory message storage
- Docker support

## API Endpoints

### Health Check
- `GET /health` - Check server status

### Messages
- `GET /api/v1/messages` - Get all messages
- `POST /api/v1/messages` - Create a new message
- `DELETE /api/v1/messages/:id` - Delete a message by ID

## Message Format

```json
{
  "id": 1,
  "username": "Alice",
  "message": "Hello, world!",
  "time": "2024-01-01 12:00:00"
}
```

## Quick Start

### Local Development

1. Clone the repository
2. Install dependencies:
   ```bash
   go mod download
   ```
3. Run the server:
   ```bash
   go run main.go
   ```
4. The server will start on `http://localhost:8080`

### Using Docker

1. Build the image:
   ```bash
   docker build -t gitchat .
   ```
2. Run the container:
   ```bash
   docker run -p 8080:8080 gitchat
   ```

## Environment Variables

- `PORT` - Server port (default: 8080)
- `GIN_MODE` - Gin mode (debug/release, default: debug)

## Usage Examples

### Create a message
```bash
curl -X POST http://localhost:8080/api/v1/messages \
  -H "Content-Type: application/json" \
  -d '{"username": "Alice", "message": "Hello, world!"}'
```

### Get all messages
```bash
curl http://localhost:8080/api/v1/messages
```

### Delete a message
```bash
curl -X DELETE http://localhost:8080/api/v1/messages/1
```

## Development

This is a basic implementation suitable for development and testing. For production use, consider:

- Using a proper database instead of in-memory storage
- Implementing authentication and authorization
- Adding input validation and rate limiting
- Using WebSockets for real-time messaging
- Adding logging and monitoring