package view_store

import (
	"github.com/ethereum/go-ethereum/common"
)

type View struct {
	viewNumber        uint64
	builderCommitment string
	stateHash         common.Hash
}

// ViewStoreBinaryTree is a binary tree
// storing the state hashes of the nitro state
// at a given view number and payload commitment
type ViewStoreBinaryTree struct {
	view  View
	Left  *ViewStoreBinaryTree
	Right *ViewStoreBinaryTree
}

/*
Insert inserts a view into the view store binary tree, on the left side of the tree all the views
with a view number and builder commitment less than the root's view number are stored. On the right side
all the views with a view number and builder commitment greater than the root's view number are stored.
*/
func Insert(root *ViewStoreBinaryTree, viewNumber uint64, builderCommitment string, stateHash common.Hash) *ViewStoreBinaryTree {
	if root == nil {
		return &ViewStoreBinaryTree{
			view: View{
				viewNumber:        viewNumber,
				builderCommitment: builderCommitment,
				stateHash:         stateHash,
			},
		}
	}
	if viewNumber < root.view.viewNumber {
		root.Left = Insert(root.Left, viewNumber, builderCommitment, stateHash)
		return root
	} else if viewNumber > root.view.viewNumber {
		root.Right = Insert(root.Right, viewNumber, builderCommitment, stateHash)
		return root
	}

	// This means that view numbers is equal to the root's view number
	// Builder commitment might be different so we insert based on that now
	if builderCommitment < root.view.builderCommitment {
		root.Left = Insert(root.Left, viewNumber, builderCommitment, stateHash)
		return root
	} else if builderCommitment > root.view.builderCommitment {
		root.Right = Insert(root.Right, viewNumber, builderCommitment, stateHash)
		return root
	}

	// This means that the view numbers are equal and the builder commitment is equal
	return root
}

/*
Search searches for a view with a given view number and builder commitment
*/
func Search(root *ViewStoreBinaryTree, viewNumber uint64, builderCommitment string) *ViewStoreBinaryTree {
	if root == nil {
		return nil
	}
	if viewNumber < root.view.viewNumber {
		return Search(root.Left, viewNumber, builderCommitment)
	} else if viewNumber > root.view.viewNumber {
		return Search(root.Right, viewNumber, builderCommitment)
	}
	if builderCommitment == root.view.builderCommitment {
		return root
	} else if builderCommitment < root.view.builderCommitment {
		return Search(root.Left, viewNumber, builderCommitment)
	} else if builderCommitment > root.view.builderCommitment {
		return Search(root.Right, viewNumber, builderCommitment)
	}
	return root
}

// Delete removes all nodes with viewNumber <= cutoff.
func Delete(root *ViewStoreBinaryTree, cutoff uint64) *ViewStoreBinaryTree {
	if root == nil {
		return nil
	}

	if root.view.viewNumber > cutoff {
		// Keep this node; only the left subtree can contain nodes to delete.
		root.Left = Delete(root.Left, cutoff)
		return root
	}

	// root.view.viewNumber <= cutoff:
	// Drop this node and its entire left subtree.
	// Continue deleting in the right subtree (may contain ties == cutoff).
	return Delete(root.Right, cutoff)
}
