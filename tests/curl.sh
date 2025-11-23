curl -X POST http://localhost:8002/g/1 \
  -H "Content-Type: application/json" \
  -d '{
    "ifFirst": true,
    "stream": true,
    "message": {
      "content": [{"type": "text", "text": ["什么是wsl技术"]}]
    }
  }' 

curl -X POST http://localhost:8002/g/1/146b0848-fbfd-49dc-997b-16f882e6fc4e \
  -H "Content-Type: application/json" \
  -d '{
    "ifFirst": false,
    "stream": true,
    "version": {"MainId": "2a58fb30-7d2f-4053-9864-2fec9b29002a"},
    "message": {
      "content": [{"type": "text", "text": ["再加6"]}]
    }
  }' 


curl -X POST http://localhost:8002/g/my-app-id/my-session-id \
-H "Content-Type: application/json" \
-d '{
"appid": {"MainId": "my-app-id"},
"session": {"MainId": "my-session-id"},
"version": {"MainId": "version-123"},
"action": "chat",
"stream": true,
"ifFirst": false,
"message": {
    "role": "user",
    "content": [
    {
        "type": "text",
        "text": ["你好"]
    }
    ],
    "createTime": "2024-01-01T00:00:00Z",
    "reqId": "req-123"
},
"model": "gpt-4",
"parentId": "parent-123",
"timezone": "Asia/Shanghai",
"languages": "zh-CN"
}'