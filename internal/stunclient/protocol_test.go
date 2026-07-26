package stunclient

import "testing"

func TestTransactionIDFromMessage(t *testing.T) {
	request, want, err := BuildBindingRequest()
	if err != nil {
		t.Fatal(err)
	}
	got, ok := TransactionIDFromMessage(request)
	if !ok || got != want {
		t.Fatalf("TransactionIDFromMessage() = %x, %v; want %x, true", got, ok, want)
	}

	request[4] = 0
	if _, ok := TransactionIDFromMessage(request); ok {
		t.Fatal("message with invalid magic cookie was accepted")
	}
}
