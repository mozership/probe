package oas

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerate_PetStore(t *testing.T) {
	spec := `
openapi: "3.0.0"
info:
  title: Pet Store
servers:
  - url: https://petstore.example.com/v1
paths:
  /pets:
    get:
      tags: [pets]
      summary: List all pets
      operationId: listPets
      parameters:
        - name: limit
          in: query
          schema:
            type: integer
      responses:
        "200":
          description: A list of pets
    post:
      tags: [pets]
      summary: Create a pet
      operationId: createPet
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
                tag:
                  type: string
      responses:
        "201":
          description: Created
  /pets/{petId}:
    get:
      tags: [pets]
      summary: Get a pet by ID
      operationId: getPet
      parameters:
        - name: petId
          in: path
          schema:
            type: integer
      responses:
        "200":
          description: A pet
`
	result, err := GenerateFromBytes([]byte(spec))
	require.NoError(t, err)

	assert.Contains(t, result, "name: Pet Store API Tests")
	assert.Contains(t, result, "base_url:")
	assert.Contains(t, result, "petstore.example.com/v1")
	assert.Contains(t, result, "id: pets")
	assert.Contains(t, result, "id: list-pets")
	assert.Contains(t, result, "id: create-pet")
	assert.Contains(t, result, "id: get-pet")
	assert.Contains(t, result, "get: /pets")
	assert.Contains(t, result, "post: /pets")
	assert.Contains(t, result, "pet_id")
	assert.Contains(t, result, "res.code == 200")
	assert.Contains(t, result, "res.code == 201")
	assert.Contains(t, result, "uses: http")
}

func TestGenerate_NoServers(t *testing.T) {
	spec := `
openapi: "3.0.0"
info:
  title: Test API
paths:
  /health:
    get:
      summary: Health check
      responses:
        "200":
          description: OK
`
	result, err := GenerateFromBytes([]byte(spec))
	require.NoError(t, err)

	assert.Contains(t, result, defaultServerURL)
}

func TestGenerate_NoTags(t *testing.T) {
	spec := `
openapi: "3.0.0"
info:
  title: Test API
servers:
  - url: https://api.example.com
paths:
  /users:
    get:
      summary: List users
      responses:
        "200":
          description: OK
  /orders:
    get:
      summary: List orders
      responses:
        "200":
          description: OK
`
	result, err := GenerateFromBytes([]byte(spec))
	require.NoError(t, err)

	// Should group by path prefix
	assert.Contains(t, result, "id: users")
	assert.Contains(t, result, "id: orders")
}

func TestGenerate_BearerAuth(t *testing.T) {
	spec := `
openapi: "3.0.0"
info:
  title: Secure API
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    bearerAuth:
      type: http
      scheme: bearer
paths:
  /data:
    get:
      summary: Get data
      responses:
        "200":
          description: OK
`
	result, err := GenerateFromBytes([]byte(spec))
	require.NoError(t, err)

	assert.Contains(t, result, "auth_token")
	assert.Contains(t, result, "Bearer {{vars.auth_token}}")
}

func TestGenerate_APIKeyAuth(t *testing.T) {
	spec := `
openapi: "3.0.0"
info:
  title: API Key API
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    apiKeyAuth:
      type: apiKey
      in: header
      name: X-API-Key
paths:
  /data:
    get:
      summary: Get data
      responses:
        "200":
          description: OK
`
	result, err := GenerateFromBytes([]byte(spec))
	require.NoError(t, err)

	assert.Contains(t, result, "api_key")
	assert.Contains(t, result, "x-api-key")
}

func TestGenerate_NoResponses(t *testing.T) {
	spec := `
openapi: "3.0.0"
info:
  title: Test API
paths:
  /data:
    get:
      summary: Get data
`
	result, err := GenerateFromBytes([]byte(spec))
	require.NoError(t, err)

	assert.Contains(t, result, "res.code >= 200 && res.code < 300")
}

func TestGenerate_RequestBody(t *testing.T) {
	spec := `
openapi: "3.0.0"
info:
  title: Test API
paths:
  /items:
    post:
      summary: Create item
      requestBody:
        content:
          application/json:
            schema:
              type: object
              properties:
                name:
                  type: string
                price:
                  type: number
                active:
                  type: boolean
      responses:
        "201":
          description: Created
`
	result, err := GenerateFromBytes([]byte(spec))
	require.NoError(t, err)

	assert.Contains(t, result, "name: example")
	assert.Contains(t, result, "active: true")
}

func TestToID(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"listPets", "list-pets"},
		{"getPetById", "get-pet-by-id"},
		{"Pet Store", "pet-store"},
		{"my_func", "my-func"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, toID(tt.input))
		})
	}
}

func TestToTitle(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"pets", "Pets"},
		{"pet-store", "Pet Store"},
		{"user_management", "User Management"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, toTitle(tt.input))
		})
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"petId", "pet_id"},
		{"userId", "user_id"},
		{"simple", "simple"},
		{"PascalCase", "pascal_case"},
	}
	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			assert.Equal(t, tt.expected, toSnakeCase(tt.input))
		})
	}
}

func TestGenerate_InvalidSpec(t *testing.T) {
	_, err := GenerateFromBytes([]byte("not yaml at all: [[["))
	assert.Error(t, err)
}

func TestGenerate_InvalidFile(t *testing.T) {
	_, err := Generate("/nonexistent/path/openapi.yml")
	assert.Error(t, err)
}

func TestGenerate_MultipartBody(t *testing.T) {
	spec := `
openapi: "3.0.0"
info:
  title: Upload API
paths:
  /upload:
    post:
      summary: Upload file
      requestBody:
        content:
          multipart/form-data:
            schema:
              type: object
              properties:
                file:
                  type: string
                  format: binary
      responses:
        "200":
          description: OK
`
	result, err := GenerateFromBytes([]byte(spec))
	require.NoError(t, err)

	assert.Contains(t, result, "multipart")
	// Should not contain body for multipart
	assert.False(t, strings.Contains(result, "body:"))
}

func TestGenerate_WildcardResponseCode(t *testing.T) {
	spec := `
openapi: "3.0.0"
info:
  title: Test API
paths:
  /data:
    get:
      summary: Get data
      responses:
        "2XX":
          description: Success
`
	result, err := GenerateFromBytes([]byte(spec))
	require.NoError(t, err)

	// 2XX is a pattern, not a concrete code; should fall back to range check
	assert.Contains(t, result, "res.code >= 200 && res.code < 300")
	assert.NotContains(t, result, "res.code == 2XX")
}

func TestGenerate_RootPath(t *testing.T) {
	spec := `
openapi: "3.0.0"
info:
  title: Test API
paths:
  /:
    get:
      summary: Root endpoint
      responses:
        "200":
          description: OK
`
	result, err := GenerateFromBytes([]byte(spec))
	require.NoError(t, err)

	// Should not produce empty job id/name
	assert.Contains(t, result, "id: api")
}

func TestGenerate_QueryParams(t *testing.T) {
	spec := `
openapi: "3.0.0"
info:
  title: Test API
paths:
  /search:
    get:
      summary: Search
      parameters:
        - name: q
          in: query
          schema:
            type: string
        - name: page
          in: query
          schema:
            type: integer
      responses:
        "200":
          description: OK
`
	result, err := GenerateFromBytes([]byte(spec))
	require.NoError(t, err)

	// Query params should be appended to the URL, not in a params field
	assert.Contains(t, result, "q=example")
	assert.Contains(t, result, "page=0")
	assert.NotContains(t, result, "params:")
}

func TestGenerate_APIKeyQueryAuth(t *testing.T) {
	spec := `
openapi: "3.0.0"
info:
  title: API Key Query API
servers:
  - url: https://api.example.com
components:
  securitySchemes:
    apiKeyAuth:
      type: apiKey
      in: query
      name: api_key
paths:
  /data:
    get:
      summary: Get data
      responses:
        "200":
          description: OK
`
	result, err := GenerateFromBytes([]byte(spec))
	require.NoError(t, err)

	assert.Contains(t, result, "api_key")
	assert.Contains(t, result, "query:")
}

func TestGenerate_DuplicateOperationID(t *testing.T) {
	spec := `
openapi: "3.0.0"
info:
  title: Test API
paths:
  /v1/items:
    get:
      tags: [items]
      operationId: getItems
      summary: Get items v1
      responses:
        "200":
          description: OK
  /v2/items:
    get:
      tags: [items]
      operationId: getItems
      summary: Get items v2
      responses:
        "200":
          description: OK
`
	result, err := GenerateFromBytes([]byte(spec))
	require.NoError(t, err)

	assert.Contains(t, result, "id: get-items")
	assert.Contains(t, result, "id: get-items-2")
}
