package main

import (
	"context"
	"encoding/json"
	"net/http"
	"sort"

	"time"

	"github.com/gorilla/mux"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var client *mongo.Client

type ResponseID struct {
	Id string `json:"id,omitempty" bson:"id,omitempty"`
}

type User struct {
	ID        primitive.ObjectID   `json:"_id,omitempty" bson:"_id,omitempty"`
	Username  string               `json:"username" bson:"username"`
	Chats     []primitive.ObjectID `json:"chats" bson:"chats"`
	CreatedAt time.Time            `json:"created_at" bson:"created_at"`
}

type Chat struct {
	ID            primitive.ObjectID   `json:"_id,omitempty" bson:"_id,omitempty"`
	Name          string               `json:"name" bson:"name"`
	Users         []primitive.ObjectID `json:"users" bson:"users"`
	Messages      []primitive.ObjectID `json:"messages" bson:"messages"`
	CreatedAt     time.Time            `json:"created_at" bson:"created_at"`
	LastMessageAt time.Time            `json:"last_message_at" bson:"last_message_at"`
}

type Message struct {
	ID        primitive.ObjectID `json:"_id,omitempty" bson:"_id,omitempty"`
	Chat      primitive.ObjectID `json:"chat" bson:"chat"`
	Author    primitive.ObjectID `json:"author" bson:"author"`
	Text      string             `json:"text" bson:"text"`
	CreatedAt time.Time          `json:"created_at" bson:"created_at"`
}

func AddUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	// receive User object from client
	var user User
	decodeErr := json.NewDecoder(r.Body).Decode(&user)
	user.Chats = []primitive.ObjectID{}
	user.CreatedAt = time.Now()

	if decodeErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + decodeErr.Error() + `"}`))
		return
	}

	// check if user with received name already exists
	var oldUser User
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	collection := client.Database("messenger").Collection("users")
	oldUserErr := collection.FindOne(ctx, bson.M{"username": user.Username}).Decode(&oldUser)
	if oldUserErr == nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message": "user with this username already exists"}`))
		return
	}

	// insert User object into DB
	result, insertErr := collection.InsertOne(ctx, user)
	if insertErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + insertErr.Error() + `"}`))
		return
	}

	// response to client with inserted User object id
	var response ResponseID
	insertedId := result.InsertedID.(primitive.ObjectID).Hex()
	response.Id = insertedId
	encodeErr := json.NewEncoder(w).Encode(response)
	if encodeErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + encodeErr.Error() + `"}`))
		return
	}
}

func AddChat(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	// receive Chat object from client
	var chat Chat
	decodeErr := json.NewDecoder(r.Body).Decode(&chat)
	chat.Messages = []primitive.ObjectID{}
	chat.CreatedAt = time.Now()

	if decodeErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + decodeErr.Error() + `"}`))
		return
	}

	// check if chat with received name already exists
	var oldChat Chat
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	collection := client.Database("messenger").Collection("chats")
	oldChatErr := collection.FindOne(ctx, bson.M{"name": chat.Name}).Decode(&oldChat)
	if oldChatErr == nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message": "chat with this name already exists"}`))
		return
	}

	usersCollection := client.Database("messenger").Collection("users")
	for _, userId := range chat.Users {
		// check if user with received id exists
		var oldUser User
		oldUserErr := usersCollection.FindOne(ctx, bson.M{"_id": userId}).Decode(&oldUser)
		if oldUserErr != nil {
			w.WriteHeader(http.StatusBadRequest)
			w.Write([]byte(`{"message": "user with id = ` + userId.Hex() + ` not exists"}`))
			return
		}
	}

	// insert Chat object into DB
	result, insertErr := collection.InsertOne(ctx, chat)
	if insertErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + insertErr.Error() + `"}`))
		return
	}

	var response ResponseID
	primitiveId := result.InsertedID.(primitive.ObjectID)
	insertedId := primitiveId.Hex()
	response.Id = insertedId

	// update info about chats within User objects in DB
	for _, userId := range chat.Users {
		_, updateErr := usersCollection.UpdateOne(ctx, bson.D{{"_id", userId}}, bson.D{{"$push", bson.D{{"chats", primitiveId}}}})
		if updateErr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message": "` + updateErr.Error() + `"}`))
			return
		}
	}

	// response to client with inserted Chat object id
	encodeErr := json.NewEncoder(w).Encode(response)
	if encodeErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + encodeErr.Error() + `"}`))
		return
	}
}

func AddMessage(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	// receive Message object from client
	var message Message
	decodeErr := json.NewDecoder(r.Body).Decode(&message)
	message.CreatedAt = time.Now()

	if decodeErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + decodeErr.Error() + `"}`))
		return
	}

	// check if chat exists
	var chat Chat
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	chatCollection := client.Database("messenger").Collection("chats")
	chatErr := chatCollection.FindOne(ctx, bson.M{"_id": message.Chat}).Decode(&chat)
	if chatErr != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message": "chat not exists"}`))
		return
	}
	// check if received user in the chat
	found := false
	for _, user := range chat.Users {
		if message.Author == user {
			found = true
			break
		}
	}
	if !found {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message": "author is not in the chat"}`))
		return
	}

	// insert Message into DB
	collection := client.Database("messenger").Collection("messages")
	result, insertErr := collection.InsertOne(ctx, message)
	if insertErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + insertErr.Error() + `"}`))
		return
	}

	var response ResponseID
	primitiveId := result.InsertedID.(primitive.ObjectID)
	insertedId := primitiveId.Hex()
	response.Id = insertedId

	// update info about messages within Chat object in DB
	chatsCollection := client.Database("messenger").Collection("chats")
	_, updateErr := chatsCollection.UpdateOne(
		ctx,
		bson.D{{"_id", message.Chat}},
		bson.D{{"$push", bson.D{{"messages", primitiveId}}}, {"$set", bson.D{{"last_message_at", time.Now()}}}},
	)
	if updateErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + updateErr.Error() + `"}`))
		return
	}

	// response to client with inserted Chat object id
	encodeErr := json.NewEncoder(w).Encode(response)
	if encodeErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + encodeErr.Error() + `"}`))
		return
	}
}

type UserID struct {
	ID primitive.ObjectID `json:"user" bson:"user"`
}

type ResponseChats struct {
	Chats []Chat `json:"chats" bson:"chats"`
}

func GetChats(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	// receive User object from client
	var userId UserID
	decodeErr := json.NewDecoder(r.Body).Decode(&userId)
	if decodeErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message": "` + decodeErr.Error() + `"}`))
		return
	}
	collection := client.Database("messenger").Collection("users")
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	chatsIds := []primitive.ObjectID{}
	var user User

	// check if user exists
	err := collection.FindOne(ctx, bson.M{"_id": userId.ID}).Decode(&user)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message": "user not exists"}`))
		return
	}
	chatsIds = user.Chats

	// get list of chats of the user
	resultChats := []Chat{}
	collection = client.Database("messenger").Collection("chats")
	for _, chatId := range chatsIds {
		var chat Chat
		findChatErr := collection.FindOne(ctx, bson.M{"_id": chatId}).Decode(&chat)
		if findChatErr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message": "` + findChatErr.Error() + `"}`))
			return
		}
		resultChats = append(resultChats, chat)
	}
	sort.Slice(resultChats, func(i, j int) bool {
		return resultChats[i].LastMessageAt.After(resultChats[j].LastMessageAt)
	})
	var response ResponseChats
	response.Chats = resultChats

	// response to client with the list of chats of the user
	encodeErr := json.NewEncoder(w).Encode(response)
	if encodeErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message 4": "` + encodeErr.Error() + `"}`))
		return
	}
}

type ChatID struct {
	ID primitive.ObjectID `json:"chat" bson:"chat"`
}

type ResponseMessages struct {
	Messages []Message `json:"messages" bson:"messages"`
}

func GetMessages(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("content-type", "application/json")
	// receive Chat object from client
	var chatId ChatID
	decodeErr := json.NewDecoder(r.Body).Decode(&chatId)
	if decodeErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message 1": "` + decodeErr.Error() + `"}`))
		return
	}
	collection := client.Database("messenger").Collection("chats")
	ctx, _ := context.WithTimeout(context.Background(), 5*time.Second)
	messagesIds := []primitive.ObjectID{}
	var chat Chat

	// check if chat exists
	err := collection.FindOne(ctx, bson.M{"_id": chatId.ID}).Decode(&chat)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"message": "chat not exists"}`))
		return
	}
	messagesIds = chat.Messages

	// get list of messages of the chat
	resultMessages := []Message{}
	collection = client.Database("messenger").Collection("messages")
	for _, messageId := range messagesIds {
		var message Message
		findMessageErr := collection.FindOne(ctx, bson.M{"_id": messageId}).Decode(&message)
		if findMessageErr != nil {
			w.WriteHeader(http.StatusInternalServerError)
			w.Write([]byte(`{"message 3": "` + findMessageErr.Error() + `"}`))
			return
		}
		resultMessages = append(resultMessages, message)
	}
	sort.Slice(resultMessages, func(i, j int) bool {
		return resultMessages[i].CreatedAt.Before(resultMessages[j].CreatedAt)
	})
	var response ResponseMessages
	response.Messages = resultMessages

	// response to client with the list of messages of the chat
	encodeErr := json.NewEncoder(w).Encode(response)
	if encodeErr != nil {
		w.WriteHeader(http.StatusInternalServerError)
		w.Write([]byte(`{"message 4": "` + encodeErr.Error() + `"}`))
		return
	}
}

func main() {
	ctx, _ := context.WithTimeout(context.Background(), 10*time.Second)
	clientOptions := options.Client().ApplyURI("mongodb://localhost:27017")
	client, _ = mongo.Connect(ctx, clientOptions)
	router := mux.NewRouter()

	router.HandleFunc("/users/add", AddUser).Methods("POST")
	router.HandleFunc("/chats/add", AddChat).Methods("POST")
	router.HandleFunc("/messages/add", AddMessage).Methods("POST")
	router.HandleFunc("/chats/get", GetChats).Methods("POST")
	router.HandleFunc("/messages/get", GetMessages).Methods("POST")
	http.ListenAndServe(":9000", router)
}
