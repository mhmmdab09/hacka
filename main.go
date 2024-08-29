package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/mux"
	_ "github.com/lib/pq"
)

// Product represents a product in the database.
type Product struct {
	ASIN              string  `json:"asin"`
	Title             string  `json:"title"`
	ImgURL            string  `json:"imgUrl"`
	ProductURL        string  `json:"productUrl"`
	Stars             float32 `json:"stars"`
	Reviews           int     `json:"reviews"`
	Price             float32 `json:"price"`
	IsBestSeller      bool    `json:"isBestSeller"`
	BoughtInLastMonth int     `json:"boughtInLastMonth"`
	CategoryName      string  `json:"categoryName"`
}

// Category represents a product category.
type Category struct {
	Name string `json:"name"`
}

// Request structures for the APIs
type AddItemToBasketRequest struct {
	ProductID string `json:"product-id"`
	UserID    string `json:"user-id"`
	BasketID  string `json:"basket-id"`
}

type CheckoutBasketRequest struct {
	UserID   string `json:"user-id"`
	BasketID string `json:"basket-id"`
}

func main() {
	// Database connection string
	connStr := os.Getenv("DATABASE_URL")
	db, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Test the database connection
	err = db.Ping()
	if err != nil {
		log.Fatal("Cannot connect to the database:", err)
	}

	r := mux.NewRouter()

	// Define the route to get all categories
	r.HandleFunc("/categories", func(w http.ResponseWriter, r *http.Request) {
		categories, err := getCategories(db)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(categories)
	}).Methods("GET")

	// Define the route to get products by category
	r.HandleFunc("/categories/{category}", func(w http.ResponseWriter, r *http.Request) {
		vars := mux.Vars(r)
		category := vars["category"]

		products, err := getProductsByCategory(db, category)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(products)
	}).Methods("GET")

	// Define the route to add an item to the basket
	r.HandleFunc("/add-item-to-basket", func(w http.ResponseWriter, r *http.Request) {
		var req AddItemToBasketRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		err := addItemToBasket(db, req.ProductID, req.UserID, req.BasketID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("Item added to basket"))
	}).Methods("POST")

	// Define the route to checkout a basket
	r.HandleFunc("/checkout-basket", func(w http.ResponseWriter, r *http.Request) {
		var req CheckoutBasketRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		err := checkoutBasket(db, req.UserID, req.BasketID)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte("Basket checked out successfully"))
	}).Methods("POST")

	fmt.Println("Server is running on port 8080...")
	log.Fatal(http.ListenAndServe(":8080", r))
}

// getCategories retrieves all distinct category names from the Products table.
func getCategories(db *sql.DB) ([]Category, error) {
	rows, err := db.Query("SELECT DISTINCT \"categoryName\" FROM \"Products\"")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var categories []Category
	for rows.Next() {
		var category Category
		if err := rows.Scan(&category.Name); err != nil {
			return nil, err
		}
		categories = append(categories, category)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return categories, nil
}

// getProductsByCategory retrieves all products from the Products table for a given category.
func getProductsByCategory(db *sql.DB, category string) ([]Product, error) {
	rows, err := db.Query("SELECT \"asin\", \"title\", \"imgUrl\", \"productUrl\", \"stars\", \"reviews\", \"price\", \"isBestSeller\", \"boughtInLastMonth\", \"categoryName\" FROM \"Products\" WHERE \"categoryName\" = $1", category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var products []Product
	for rows.Next() {
		var product Product
		if err := rows.Scan(&product.ASIN, &product.Title, &product.ImgURL, &product.ProductURL, &product.Stars, &product.Reviews, &product.Price, &product.IsBestSeller, &product.BoughtInLastMonth, &product.CategoryName); err != nil {
			return nil, err
		}
		products = append(products, product)
	}

	if err = rows.Err(); err != nil {
		return nil, err
	}

	return products, nil
}

// addItemToBasket adds an item to the basket and updates the ProductCounts table
func addItemToBasket(db *sql.DB, productID, userID, basketID string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Check if the product exists and has sufficient count
	var count int
	err = tx.QueryRow("SELECT \"count\" FROM \"ProductCounts\" WHERE \"asin\" = $1", productID).Scan(&count)
	if err != nil {
		if err == sql.ErrNoRows {
			return fmt.Errorf("product not found")
		}
		return err
	}

	if count <= 0 {
		return fmt.Errorf("product out of stock")
	}

	// Insert the product into the Baskets table
	_, err = tx.Exec("INSERT INTO \"Baskets\" (\"BasketId\", \"ProductId\", \"UserId\", \"IsCheckedOut\") VALUES ($1, $2, $3, $4)",
		basketID, productID, userID, false)
	if err != nil {
		return err
	}

	// Decrement the product count
	_, err = tx.Exec("UPDATE \"ProductCounts\" SET \"count\" = \"count\" - 1 WHERE \"asin\" = $1", productID)
	if err != nil {
		return err
	}

	return tx.Commit()
}

// checkoutBasket checks out the basket and marks all items as checked out
func checkoutBasket(db *sql.DB, userID, basketID string) error {
	_, err := db.Exec("UPDATE \"Baskets\" SET \"IsCheckedOut\" = true WHERE \"UserId\" = $1 AND \"BasketId\" = $2", userID, basketID)
	return err
}

// GenerateRandomUserID generates a random UserID for each session (for example usage)
func GenerateRandomUserID() string {
	rand.Seed(time.Now().UnixNano())
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, 8)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}
