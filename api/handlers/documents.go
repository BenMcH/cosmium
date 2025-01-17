package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"

	jsonpatch "github.com/evanphx/json-patch/v5"
	"github.com/gin-gonic/gin"
	"github.com/pikami/cosmium/internal/constants"
	"github.com/pikami/cosmium/internal/logger"
	"github.com/pikami/cosmium/internal/repositories"
	repositorymodels "github.com/pikami/cosmium/internal/repository_models"
)

func GetAllDocuments(c *gin.Context) {
	databaseId := c.Param("databaseId")
	collectionId := c.Param("collId")

	documents, status := repositories.GetAllDocuments(databaseId, collectionId)
	if status == repositorymodels.StatusOk {
		collection, _ := repositories.GetCollection(databaseId, collectionId)

		c.Header("x-ms-item-count", fmt.Sprintf("%d", len(documents)))
		c.IndentedJSON(http.StatusOK, gin.H{
			"_rid":      collection.ID,
			"Documents": documents,
			"_count":    len(documents),
		})
		return
	}

	c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "Unknown error"})
}

func GetDocument(c *gin.Context) {
	databaseId := c.Param("databaseId")
	collectionId := c.Param("collId")
	documentId := c.Param("docId")

	document, status := repositories.GetDocument(databaseId, collectionId, documentId)
	if status == repositorymodels.StatusOk {
		c.IndentedJSON(http.StatusOK, document)
		return
	}

	if status == repositorymodels.StatusNotFound {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "NotFound"})
		return
	}

	c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "Unknown error"})
}

func DeleteDocument(c *gin.Context) {
	databaseId := c.Param("databaseId")
	collectionId := c.Param("collId")
	documentId := c.Param("docId")

	status := repositories.DeleteDocument(databaseId, collectionId, documentId)
	if status == repositorymodels.StatusOk {
		c.Status(http.StatusNoContent)
		return
	}

	if status == repositorymodels.StatusNotFound {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "NotFound"})
		return
	}

	c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "Unknown error"})
}

// TODO: Maybe move "replace" logic to repository
func ReplaceDocument(c *gin.Context) {
	databaseId := c.Param("databaseId")
	collectionId := c.Param("collId")
	documentId := c.Param("docId")

	var requestBody map[string]interface{}
	if err := c.BindJSON(&requestBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	status := repositories.DeleteDocument(databaseId, collectionId, documentId)
	if status == repositorymodels.StatusNotFound {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "NotFound"})
		return
	}

	createdDocument, status := repositories.CreateDocument(databaseId, collectionId, requestBody)
	if status == repositorymodels.Conflict {
		c.IndentedJSON(http.StatusConflict, gin.H{"message": "Conflict"})
		return
	}

	if status == repositorymodels.StatusOk {
		c.IndentedJSON(http.StatusCreated, createdDocument)
		return
	}

	c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "Unknown error"})
}

func PatchDocument(c *gin.Context) {
	databaseId := c.Param("databaseId")
	collectionId := c.Param("collId")
	documentId := c.Param("docId")

	document, status := repositories.GetDocument(databaseId, collectionId, documentId)
	if status == repositorymodels.StatusNotFound {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "NotFound"})
		return
	}

	var requestBody map[string]interface{}
	if err := c.BindJSON(&requestBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	operations := requestBody["operations"]
	operationsBytes, err := json.Marshal(operations)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": "Could not decode operations"})
		return
	}

	patch, err := jsonpatch.DecodePatch(operationsBytes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	currentDocumentBytes, err := json.Marshal(document)
	if err != nil {
		logger.Error("Failed to marshal existing document:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to marshal existing document"})
		return
	}

	modifiedDocumentBytes, err := patch.Apply(currentDocumentBytes)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	var modifiedDocument map[string]interface{}
	err = json.Unmarshal(modifiedDocumentBytes, &modifiedDocument)
	if err != nil {
		logger.Error("Failed to unmarshal modified document:", err)
		c.JSON(http.StatusInternalServerError, gin.H{"message": "Failed to unmarshal modified document"})
		return
	}

	if modifiedDocument["id"] != document["id"] {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"message": "The ID field cannot be modified"})
		return
	}

	status = repositories.DeleteDocument(databaseId, collectionId, documentId)
	if status == repositorymodels.StatusNotFound {
		c.IndentedJSON(http.StatusNotFound, gin.H{"message": "NotFound"})
		return
	}

	createdDocument, status := repositories.CreateDocument(databaseId, collectionId, modifiedDocument)
	if status == repositorymodels.Conflict {
		c.IndentedJSON(http.StatusConflict, gin.H{"message": "Conflict"})
		return
	}

	if status == repositorymodels.StatusOk {
		c.IndentedJSON(http.StatusCreated, createdDocument)
		return
	}

	c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "Unknown error"})
}

func DocumentsPost(c *gin.Context) {
	databaseId := c.Param("databaseId")
	collectionId := c.Param("collId")

	var requestBody map[string]interface{}
	if err := c.BindJSON(&requestBody); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"message": err.Error()})
		return
	}

	query := requestBody["query"]
	if query != nil {
		if c.GetHeader("x-ms-cosmos-is-query-plan-request") != "" {
			c.IndentedJSON(http.StatusOK, constants.QueryPlanResponse)
			return
		}

		var queryParameters map[string]interface{}
		if paramsArray, ok := requestBody["parameters"].([]interface{}); ok {
			queryParameters = parametersToMap(paramsArray)
		}

		docs, status := repositories.ExecuteQueryDocuments(databaseId, collectionId, query.(string), queryParameters)
		if status != repositorymodels.StatusOk {
			// TODO: Currently we return everything if the query fails
			GetAllDocuments(c)
			return
		}

		collection, _ := repositories.GetCollection(databaseId, collectionId)
		c.Header("x-ms-item-count", fmt.Sprintf("%d", len(docs)))
		c.IndentedJSON(http.StatusOK, gin.H{
			"_rid":      collection.ResourceID,
			"Documents": docs,
			"_count":    len(docs),
		})
		return
	}

	if requestBody["id"] == "" {
		c.JSON(http.StatusBadRequest, gin.H{"message": "BadRequest"})
		return
	}

	isUpsert, _ := strconv.ParseBool(c.GetHeader("x-ms-documentdb-is-upsert"))
	if isUpsert {
		repositories.DeleteDocument(databaseId, collectionId, requestBody["id"].(string))
	}

	createdDocument, status := repositories.CreateDocument(databaseId, collectionId, requestBody)
	if status == repositorymodels.Conflict {
		c.IndentedJSON(http.StatusConflict, gin.H{"message": "Conflict"})
		return
	}

	if status == repositorymodels.StatusOk {
		c.IndentedJSON(http.StatusCreated, createdDocument)
		return
	}

	c.IndentedJSON(http.StatusInternalServerError, gin.H{"message": "Unknown error"})
}

func parametersToMap(pairs []interface{}) map[string]interface{} {
	result := make(map[string]interface{})

	for _, pair := range pairs {
		if pairMap, ok := pair.(map[string]interface{}); ok {
			result[pairMap["name"].(string)] = pairMap["value"]
		}
	}

	return result
}
