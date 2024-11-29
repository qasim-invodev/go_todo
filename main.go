package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/middleware"
	"github.com/thedevsaddam/renderer"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var rnd *renderer.Render
var db *mongo.Database
var client *mongo.Client

const (
	hostName       string = "mongodb://127.0.0.1:27017"
	dbName         string = "demo_todo"
	collectionName string = "todo"
	port           string = ":9000"
)

type (
	todoModel struct {
		ID        primitive.ObjectID `bson:"_id,omitempty"`
		Title     string             `bson:"title"`
		Completed bool               `bson:"completed"`
		CreatedAt time.Time          `bson:"createAt"`
	}

	todo struct {
		ID        string    `json:"id"`
		Title     string    `json:"title"`
		Completed string    `json:"completed"`
		CreatedAt time.Time `json:"created_at"`
	}
)

func init() {
	rnd = renderer.New()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// Create a MongoDB client
	var err error
	client, err = mongo.Connect(ctx, options.Client().ApplyURI(hostName))
	if err != nil {
		log.Fatalf("Failed to connect to MongoDB: %v\n", err)
	}

	// Verify the connection
	if err = client.Ping(ctx, nil); err != nil {
		log.Fatalf("Failed to ping MongoDB: %v\n", err)
	}

	log.Println("Successfully connected to MongoDB")
	db = client.Database(dbName)
}

func homeHandler(w http.ResponseWriter, r *http.Request) {
	err := rnd.Template(w, http.StatusOK, []string{"static/home.tpl"}, nil)
	checkErr(err)
}

func fetchTodos(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := db.Collection(collectionName)
	cursor, err := collection.Find(ctx, bson.M{})
	if err != nil {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{"message": "could not fetch todos", "error": err.Error()})
		return
	}
	defer cursor.Close(ctx)

	var todos []todoModel
	if err := cursor.All(ctx, &todos); err != nil {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{"message": "could not decode todos", "error": err.Error()})
		return
	}

	todoList := []todo{}
	for _, t := range todos {
		todoList = append(todoList, todo{
			ID:        t.ID.Hex(),
			Title:     t.Title,
			Completed: strconv.FormatBool(t.Completed),
			CreatedAt: t.CreatedAt,
		})
	}
	rnd.JSON(w, http.StatusOK, renderer.M{"data": todoList})
}

func createTodo(w http.ResponseWriter, r *http.Request) {
	var t todo
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{"message": "invalid request", "error": err.Error()})
		return
	}

	if t.Title == "" {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{"message": "title is required", "error": "bad request"})
		return
	}

	tm := todoModel{
		ID:        primitive.NewObjectID(),
		Title:     t.Title,
		Completed: false,
		CreatedAt: time.Now(),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := db.Collection(collectionName)
	res, err := collection.InsertOne(ctx, tm)
	if err != nil {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{"message": "could not create todo", "error": err.Error()})
		return
	}

	rnd.JSON(w, http.StatusCreated, renderer.M{"message": "todo created successfully", "todo_id": res.InsertedID})
}

func deleteTodo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !primitive.IsValidObjectID(id) {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{"message": "invalid id", "error": "bad request"})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := db.Collection(collectionName)
	objID, _ := primitive.ObjectIDFromHex(id)
	res, err := collection.DeleteOne(ctx, bson.M{"_id": objID})
	if err != nil || res.DeletedCount == 0 {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{"message": "could not delete todo", "error": err.Error()})
		return
	}

	rnd.JSON(w, http.StatusOK, renderer.M{"message": "todo deleted successfully"})
}

func updateTodo(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if !primitive.IsValidObjectID(id) {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{"message": "invalid id", "error": "bad request"})
		return
	}

	var t todo
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{"message": "invalid request", "error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	collection := db.Collection(collectionName)
	objID, _ := primitive.ObjectIDFromHex(id)
	update := bson.M{"$set": bson.M{"title": t.Title, "completed": t.Completed}}
	res, err := collection.UpdateOne(ctx, bson.M{"_id": objID}, update)
	if err != nil || res.MatchedCount == 0 {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{"message": "could not update todo", "error": err.Error()})
		return
	}

	rnd.JSON(w, http.StatusOK, renderer.M{"message": "todo updated successfully"})
}

func main() {
	stopChan := make(chan os.Signal, 1)
	signal.Notify(stopChan, os.Interrupt)
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Get("/", homeHandler)
	r.Mount("/todo", todoHandlers())

	srv := &http.Server{
		Addr:         port,
		Handler:      r,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}
	go func() {
		log.Println("listening on port: ", port)
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("listen:%s\n", err)
		}
	}()

	<-stopChan
	log.Println("shutting down server...")
	if client != nil {
		client.Disconnect(context.Background())
		log.Println("Closed MongoDB connection")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	srv.Shutdown(ctx)
	defer cancel()
	log.Println("server gracefully stopped")
}

func todoHandlers() http.Handler {
	rg := chi.NewRouter()
	rg.Group(func(r chi.Router) {
		r.Get("/", fetchTodos)
		r.Post("/", createTodo)
		r.Put("/{id}", updateTodo)
		r.Delete("/{id}", deleteTodo)
	})
	return rg
}

func checkErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
