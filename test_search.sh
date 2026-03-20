#!/bin/bash

# 测试搜索API的脚本

BASE_URL="http://localhost:8080/api/v1"

echo "=== 测试1: 正常搜索请求 ==="
curl -X POST "$BASE_URL/search" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "测试查询",
    "knowledge_base_id": 1,
    "top_k": 5
  }'
echo -e "\n"

echo "=== 测试2: 不传top_k参数 ==="
curl -X POST "$BASE_URL/search" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "测试查询",
    "knowledge_base_id": 1
  }'
echo -e "\n"

echo "=== 测试3: top_k = 0 ==="
curl -X POST "$BASE_URL/search" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "测试查询",
    "knowledge_base_id": 1,
    "top_k": 0
  }'
echo -e "\n"

echo "=== 测试4: top_k 超过限制 ==="
curl -X POST "$BASE_URL/search" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "测试查询",
    "knowledge_base_id": 1,
    "top_k": 100
  }'
echo -e "\n"

echo "=== 测试5: 缺少必填参数 query ==="
curl -X POST "$BASE_URL/search" \
  -H "Content-Type: application/json" \
  -d '{
    "knowledge_base_id": 1,
    "top_k": 5
  }'
echo -e "\n"

echo "=== 测试6: 缺少必填参数 knowledge_base_id ==="
curl -X POST "$BASE_URL/search" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "测试查询",
    "top_k": 5
  }'
echo -e "\n"

echo "=== 测试7: knowledge_base_id = 0 (应该失败) ==="
curl -X POST "$BASE_URL/search" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "测试查询",
    "knowledge_base_id": 0,
    "top_k": 5
  }'
echo -e "\n"

echo "=== 测试8: 空查询字符串 ==="
curl -X POST "$BASE_URL/search" \
  -H "Content-Type: application/json" \
  -d '{
    "query": "",
    "knowledge_base_id": 1,
    "top_k": 5
  }'
echo -e "\n"
