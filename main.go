package main

import (
	"api/db"
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"
	_ "github.com/go-sql-driver/mysql"
	"github.com/google/generative-ai-go/genai"
	"google.golang.org/api/option"
)

func main() {
	db.InitDB()

	r := gin.Default()

	r.POST("/register", db.RegisterHandler)
	r.POST("/login", db.LoginHandler)

	r.GET("/recipes", db.ValidateToken, func(c *gin.Context) {
		userId := c.MustGet("user_id").(string)

		recipes, err := db.GetFoodRecipes(userId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil resep"})
			return
		}

		if len(recipes) == 0 {
			c.JSON(http.StatusOK, gin.H{"message": "Tidak ada resep yang ditemukan"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"recipes": recipes})
	})

	r.GET("/users", db.ValidateToken, func(c *gin.Context) {
		role := c.MustGet("role").(string)

		if role != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "Anda tidak memiliki izin untuk mengakses daftar pengguna"})
			return
		}

		users, err := db.GetUsers()
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil data pengguna"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"users": users})
	})

	r.POST("/foods", db.ValidateToken, func(c *gin.Context) {
		var req db.AddFoodRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Format request salah"})
			return
		}

		userId := c.MustGet("user_id").(string)
		err := db.AddFood(req.Name, req.ExpiryDate, userId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan makanan"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Makanan berhasil disimpan"})
	})

	r.DELETE("/recipes/:id", db.ValidateToken, func(c *gin.Context) {

		id := c.Param("id")

		userId := c.MustGet("user_id").(string)

		var recipeUserId string
		err := db.DB.QueryRow("SELECT user_id FROM food_recipes WHERE id = ?", id).Scan(&recipeUserId)
		if err != nil || recipeUserId != userId {

			c.JSON(http.StatusForbidden, gin.H{"error": "Resep tidak ditemukan atau tidak milik Anda"})
			return
		}

		_, err = db.DB.Exec("DELETE FROM food_recipes WHERE id = ?", id)
		if err != nil {

			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menghapus resep"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Resep berhasil dihapus"})
	})

	r.DELETE("/foods/:id", db.ValidateToken, func(c *gin.Context) {
		id := c.Param("id")
		userId := c.MustGet("user_id").(string)

		var foodUserId string
		err := db.DB.QueryRow("SELECT user_id FROM foods WHERE id = ?", id).Scan(&foodUserId)
		if err != nil || foodUserId != userId {
			c.JSON(http.StatusForbidden, gin.H{"error": "Makanan tidak ditemukan atau tidak milik Anda"})
			return
		}

		_, err = db.DB.Exec("DELETE FROM foods WHERE id = ?", id)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menghapus makanan"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"message": "Makanan berhasil dihapus"})
	})

	r.GET("/foods", db.ValidateToken, func(c *gin.Context) {
		userId := c.MustGet("user_id").(string)

		foods, err := db.GetFoods(userId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil data makanan"})
			return
		}
		c.JSON(http.StatusOK, foods)
	})

	r.GET("/login-logs", db.ValidateToken, func(c *gin.Context) {

		userId, exists := c.Get("user_id")
		if !exists {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "User ID not found"})
			return
		}

		role, exists := c.Get("role")
		if !exists || role != "admin" {
			c.JSON(http.StatusForbidden, gin.H{"error": "Anda tidak memiliki izin untuk mengakses log login pengguna"})
			return
		}

		log.Printf("Fetching login logs for user_id: %v, role: %v", userId, role)

		logs, err := db.GetLoginLogs(userId.(string))
		if err != nil {
			log.Printf("Error while fetching login logs: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mengambil data log login"})
			return
		}

		c.JSON(http.StatusOK, gin.H{"login_logs": logs})
	})
	r.POST("/recipe", db.ValidateToken, func(c *gin.Context) {
		var req db.RecipeRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Format request salah"})
			return
		}

		userId := c.MustGet("user_id").(string)

		// Ambil nama bahan makanan berdasarkan FoodID yang diterima
		var foodNames []string
		for _, foodID := range req.FoodID {
			var foodName string
			err := db.DB.QueryRow("SELECT name FROM foods WHERE id = ? AND user_id = ?", foodID, userId).Scan(&foodName)
			if err != nil {
				c.JSON(http.StatusNotFound, gin.H{"error": "Makanan tidak ditemukan atau tidak milik Anda"})
				return
			}
			foodNames = append(foodNames, foodName)
		}

		// Gabungkan nama bahan makanan untuk digunakan dalam permintaan ke AI
		ingredients := strings.Join(foodNames, ", ")

		// Ambil API key dari environment variable
		apiKey := os.Getenv("API_KEY")
		if apiKey == "" {
			log.Fatal("API key tidak ditemukan! Pastikan sudah diset di environment variable.")
		}
		ctx := context.Background()
		client, err := genai.NewClient(ctx, option.WithAPIKey(apiKey))
		if err != nil {
			log.Fatalf("Error creating AI client: %v", err)
		}
		defer client.Close()

		// Buat prompt untuk AI berdasarkan bahan makanan yang dipilih
		userInput := fmt.Sprintf("anggap dirimu adalah chef. Berikan resep gampang dan berikan ukuran pasti tapi enak untuk bahan-bahan berikut: %s. Di terakhir tuliskan by Chef SaveBite", ingredients)

		// Pilih model AI yang digunakan
		model := client.GenerativeModel("gemini-1.5-flash")
		resp, err := model.GenerateContent(ctx, genai.Text(userInput))
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal mendapatkan resep dari AI"})
			return
		}

		// Periksa apakah AI mengembalikan hasil yang valid
		if len(resp.Candidates) == 0 || resp.Candidates[0].Content == nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "AI tidak mengembalikan hasil yang valid"})
			return
		}

		// Gabungkan hasil dari AI menjadi satu string
		var output strings.Builder
		for _, part := range resp.Candidates[0].Content.Parts {
			output.WriteString(fmt.Sprintf("%v\n", part))
		}

		// Simpan resep ke dalam database
		err = db.AddFoodRecipe(req.FoodID, output.String(), userId)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "Gagal menyimpan resep ke database"})
			return
		}

		// Kirimkan resep sebagai response
		c.JSON(http.StatusOK, db.RecipeResponse{Recipe: output.String()})
	})

	r.Run(":8080")
}
