package view_store

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
)

func mkHash(s string) common.Hash { return common.BytesToHash([]byte(s)) }

func MakeInitialTree(t *testing.T) *ViewStoreBinaryTree {
	root := Insert(nil, 5, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgA_", mkHash("1"))
	root = Insert(root, 2, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgB_", mkHash("2"))
	root = Insert(root, 7, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgC_", mkHash("3"))
	return root
}

func TestEspressoViewStoreInsert(t *testing.T) {
	t.Run("Insert view on the left side of the tree", func(t *testing.T) {
		root := MakeInitialTree(t)

		// Now insert a view which has a view number which is less than the root's view number
		root = Insert(root, 0, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_", mkHash("0"))

		// Search for view number 2
		viewStoreForViewNumvber2 := Search(root, 2, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgB_")
		viewStoreForViewNumvber0 := Search(root, 0, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_")

		// Check that left child has view number 0
		if viewStoreForViewNumvber2.Left.View.viewNumber != 0 {
			t.Errorf("Expected left child's view number to be 0, got %d", viewStoreForViewNumvber2.Left.View.viewNumber)
		}

		if viewStoreForViewNumvber0.View.viewNumber != 0 {
			t.Errorf("Expected root's view number to be 0, got %d", viewStoreForViewNumvber0.View.viewNumber)
		}
		if viewStoreForViewNumvber0.View.builderCommitment != "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_" {
			t.Errorf("Expected root's builder commitment to be BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_, got %s", viewStoreForViewNumvber0.View.builderCommitment)
		}

	})

	t.Run("Insert view on the right side of the tree", func(t *testing.T) {
		root := MakeInitialTree(t)

		// Now insert a view which has a view number which is less than the root's view number
		root = Insert(root, 9, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_", mkHash("9"))

		// Now check that its parent should be view number 7
		viewStoreForViewNumber7 := Search(root, 7, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgC_")

		// Check that the right side of view store 7 is the inserted view
		if viewStoreForViewNumber7.Right.View.viewNumber != 9 {
			t.Errorf("Expected right side of view store 7 to be 9, got %d", viewStoreForViewNumber7.Right.View.viewNumber)
		}
		if viewStoreForViewNumber7.Right.View.builderCommitment != "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_" {
			t.Errorf("Expected right side of view store 7 to be BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_, got %s", viewStoreForViewNumber7.Right.View.builderCommitment)
		}

	})

	t.Run("Insert a view which has the same view number but higher builder commitment", func(t *testing.T) {
		root := MakeInitialTree(t)

		// Now insert a view which has the same view number but higher builder commitment
		root = Insert(root, 5, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_", mkHash("4"))

		// Now check that its parent view should be 5 view store with lower builder commitment
		viewStoreForViewNumber5HigherBuilderCommitment := Search(root, 5, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_")
		viewStoreFor7ViewNumber := Search(root, 7, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgC_")

		// Check that the lower builder commitment view store 5 exists on the left side of view store 7
		if viewStoreFor7ViewNumber.Left.View.viewNumber != 5 {
			t.Errorf("Expected left side of view store for view number 7 to have view number 5, got %d", viewStoreFor7ViewNumber.Left.View.viewNumber)
		}
		if viewStoreFor7ViewNumber.Left.View.builderCommitment != "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_" {
			t.Errorf("Expected left side of view store for view number 7 to have builder commitment BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_, got %s", viewStoreForViewNumber5HigherBuilderCommitment.Left.View.builderCommitment)
		}
	})

	t.Run("Insert a view which has the same view number but lower builder commitment", func(t *testing.T) {
		root := MakeInitialTree(t)

		// Now insert a view which has the same view number but lower builder commitment
		root = Insert(root, 5, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgA_", mkHash("4"))

		viewStoreFor2ViewNumber := Search(root, 2, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgB_")

		// Check that the lower builder commitment view store 5 exists on the left side of view store 7
		if viewStoreFor2ViewNumber.Right.View.viewNumber != 5 {
			t.Errorf("Expected right side of view store for view number 2 to have view number 5, got %d", viewStoreFor2ViewNumber.Left.View.viewNumber)
		}
		if viewStoreFor2ViewNumber.Right.View.builderCommitment != "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgA_" {
			t.Errorf("Expected right side of view store for view number 2 to have builder commitment BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgA_, got %s", viewStoreFor2ViewNumber.Left.View.builderCommitment)
		}
	})

}

func TestEspressoViewStoreSearch(t *testing.T) {
	t.Run("Search view on the left side of the tree", func(t *testing.T) {
		root := MakeInitialTree(t)

		// Now insert a view which has a view number which is less than the root's view number
		root = Insert(root, 0, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_", mkHash("0"))

		// Search for view number 2
		viewStoreForViewNumber0 := Search(root, 0, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_")

		if viewStoreForViewNumber0.View.viewNumber != 0 {
			t.Errorf("Expected root's view number to be 0, got %d", viewStoreForViewNumber0.View.viewNumber)
		}
		if viewStoreForViewNumber0.View.builderCommitment != "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_" {
			t.Errorf("Expected root's builder commitment to be BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_, got %s", viewStoreForViewNumber0.View.builderCommitment)
		}
	})

	t.Run("Search view on the right side of the tree", func(t *testing.T) {
		root := MakeInitialTree(t)

		root = Insert(root, 10, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgB_", mkHash("2"))
		// Search for view number 2
		viewStoreForViewNumber2 := Search(root, 10, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgB_")

		if viewStoreForViewNumber2.View.viewNumber != 10 {
			t.Errorf("Expected right child's view number to be 10, got %d", viewStoreForViewNumber2.Right.View.viewNumber)
		}
		if viewStoreForViewNumber2.View.builderCommitment != "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgB_" {
			t.Errorf("Expected right child's builder commitment to be BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgB_, got %s", viewStoreForViewNumber2.Right.View.builderCommitment)
		}
	})

	t.Run("search for a view number which is equal to one of the view numbers but has a greater builder commitment", func(t *testing.T) {
		root := MakeInitialTree(t)

		// Insert a view which has a view number which is equal to the root's view number
		// but has a greater builder commitment
		root = Insert(root, 2, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_", mkHash("4"))

		// Search for view number 2
		viewStoreForViewNumber2 := Search(root, 2, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_")

		if viewStoreForViewNumber2.View.viewNumber != 2 {
			t.Errorf("Expected  view number to be 2, got %d", viewStoreForViewNumber2.Right.View.viewNumber)
		}
		if viewStoreForViewNumber2.View.builderCommitment != "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_" {
			t.Errorf("Expected  builder commitment to be BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_, got %s", viewStoreForViewNumber2.Right.View.builderCommitment)
		}
	})

	t.Run("search for a view number which is equal to one of the view numbers but has a lower builder commitment", func(t *testing.T) {
		root := MakeInitialTree(t)

		// Insert a view which has a view number which is equal to the root's view number
		// but has a lower builder commitment
		root = Insert(root, 2, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgA_", mkHash("4"))

		// Search for view number 2
		viewStoreForViewNumber2 := Search(root, 2, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgA_")

		if viewStoreForViewNumber2.View.viewNumber != 2 {
			t.Errorf("Expected  view number to be 2, got %d", viewStoreForViewNumber2.View.viewNumber)
		}
		if viewStoreForViewNumber2.View.builderCommitment != "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgA_" {
			t.Errorf("Expected  builder commitment to be BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgA_, got %s", viewStoreForViewNumber2.View.builderCommitment)
		}
	})

	t.Run("search for a view number which doesnt exist", func(t *testing.T) {
		root := MakeInitialTree(t)

		// Search for view number 2
		viewStore := Search(root, 3, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgB_")

		if viewStore != nil {
			t.Errorf("Expected view store to be nil, got %v", viewStore)
		}
	})

	t.Run("search for a view which has a given view number but builder commitment which doesnt exist", func(t *testing.T) {
		root := MakeInitialTree(t)

		// Search for view number 2
		viewStore := Search(root, 2, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgK_")

		if viewStore != nil {
			t.Errorf("Expected view store to be nil, got %v", viewStore)
		}
	})
}

func TestViewStoreDelete(t *testing.T) {
	t.Run("Delete a view which has a view number less than the root's view number", func(t *testing.T) {
		root := MakeInitialTree(t)

		// Delete view number 0
		root = Delete(root, 2)

		// Check that now no element exits on the left side of the tree
		if root.Left != nil {
			t.Errorf("Expected left child to be nil, got %v", root.Left)
		}
	})

	t.Run("Delete a view which has a view number greater than the root's view number", func(t *testing.T) {
		root := MakeInitialTree(t)

		// Delete view number 0
		root = Delete(root, 7)

		// It should have deleted the whole tree
		if root != nil {
			t.Errorf("Expected root to be nil, got %v", root)
		}

		root = MakeInitialTree(t)

		// Insert a view number 10 and 8
		root = Insert(root, 10, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgB_", mkHash("2"))
		root = Insert(root, 8, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgB_", mkHash("2"))

		// Delete view number 7
		root = Delete(root, 7)

		// root view number should be 10
		if root.View.viewNumber != 10 {
			t.Errorf("Expected root view number to be 10, got %d", root.View.viewNumber)
		}

		// Search and there should be no view with view number 7
		viewStore := Search(root, 7, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgB_")
		if viewStore != nil {
			t.Errorf("Expected view store to be nil, got %v", viewStore)
		}
	})

	t.Run("Delete a view which has a view number equal to one of the views but higher builder commitment", func(t *testing.T) {
		root := MakeInitialTree(t)

		// Insert a view number 2 but with a higher builder commitment
		root = Insert(root, 2, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgF_", mkHash("4"))
		// Delete view number 0
		root = Delete(root, 2)

		// There should now be no left child of the root
		if root.Left != nil {
			t.Errorf("Expected left child to be nil, got %v", root.Left)
		}
	})

	t.Run("Delete a view which has a view number equal to one of the views but lower builder commitment", func(t *testing.T) {
		root := MakeInitialTree(t)

		// Insert a view number 2 but with a lower builder commitment
		root = Insert(root, 7, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgA_", mkHash("4"))
		// Delete view number 0
		root = Delete(root, 7)

		// now the whole tree should be deleted
		if root != nil {
			t.Errorf("Expected root to be nil, got %v", root)
		}

		root = MakeInitialTree(t)
		// Insert a view number 10 and 8
		root = Insert(root, 10, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgB_", mkHash("2"))
		root = Insert(root, 8, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgB_", mkHash("2"))

		// Insert a lower builder commitment for view 8 and delete view number 8
		root = Insert(root, 8, "BUILDER_COMMITMENT~tEvs0rxqOiMCvfe2R0omNNaphSlUiEDrb2q0IZpRcgA_", mkHash("2"))
		root = Delete(root, 8)

		// root view number should be 10
		if root.View.viewNumber != 10 {
			t.Errorf("Expected root view number to be 10, got %d", root.View.viewNumber)
		}
	})

}
