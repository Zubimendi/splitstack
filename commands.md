# SplitStack Local Development Commands

This guide provides the most frequently used commands to start the server, run tests, and interact with the API endpoints locally using `curl`.

## 🚀 Server & Database Management

These commands are defined in the `Makefile` and are used to manage the local environment:

```bash
# Start the PostgreSQL database in a Docker container
make up

# Run database migrations (must run after `make up`)
make migrate

# Seed the database with initial test data
make seed

# Start the Go API server
make run

# Run unit tests
make test

# Stop the database and remove volumes
make down
```

## 🧪 Testing Endpoints with cURL

Once the server is running (`make run`), you can test the endpoints using the following `curl` commands.

*Note: Replace `<user1_id>`, `<user2_id>`, and `<group_id>` with actual UUIDs returned by the API.*

### 1. Create a User
```bash
curl -X POST http://localhost:8080/users \
  -H "Authorization: Bearer dev-secret-key" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Alice",
    "email": "alice@example.com"
  }'
```

### 2. Create a Group
```bash
curl -X POST http://localhost:8080/groups \
  -H "Authorization: Bearer dev-secret-key" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "Weekend Trip",
    "currency": "USD",
    "memberUserIds": [
        "<user1_id>",
        "<user2_id>"
    ]
  }'
```

### 3. Add an Expense
```bash
curl -X POST http://localhost:8080/groups/<group_id>/expenses \
  -H "Authorization: Bearer dev-secret-key" \
  -H "Content-Type: application/json" \
  -d '{
    "description": "Dinner",
    "totalAmountCents": 10000,
    "currency": "USD",
    "paidByUserId": "<user1_id>",
    "splits": []
  }'
```

### 4. Record a Settlement
```bash
curl -X POST http://localhost:8080/groups/<group_id>/settlements \
  -H "Authorization: Bearer dev-secret-key" \
  -H "Content-Type: application/json" \
  -d '{
    "fromUserId": "<user2_id>",
    "toUserId": "<user1_id>",
    "amountCents": 5000
  }'
```

### 5. Get Group Balances
```bash
curl -X GET http://localhost:8080/groups/<group_id>/balances \
  -H "Authorization: Bearer dev-secret-key"
```

### 6. Get Verified Balances
```bash
curl -X GET http://localhost:8080/groups/<group_id>/balances/verified \
  -H "Authorization: Bearer dev-secret-key"
```

### 7. Get Settlement Plan
```bash
curl -X GET http://localhost:8080/groups/<group_id>/settlement-plan \
  -H "Authorization: Bearer dev-secret-key"
```
