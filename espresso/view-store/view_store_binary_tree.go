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
// TODO: should we store this in the database?
type ViewStoreBinaryTree struct {
	view  View
	Left  *ViewStoreBinaryTree
	Right *ViewStoreBinaryTree
}

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
	} else if viewNumber > root.view.viewNumber {
		root.Right = Insert(root.Right, viewNumber, builderCommitment, stateHash)
	}

	// This means that view numbers is equal to the root's view number
	// Payload commitment might be different so we insert based on that now
	if builderCommitment < root.view.builderCommitment {
		root.Left = Insert(root.Left, viewNumber, builderCommitment, stateHash)
	} else if builderCommitment > root.view.builderCommitment {
		root.Right = Insert(root.Right, viewNumber, builderCommitment, stateHash)
	}

	// This means that the view numbers are equal and the payload commitment is equal
	return root
}

func Search(root *ViewStoreBinaryTree, viewNumber uint64, builderCommitment string) *View {
	if root == nil {
		return nil
	}
	if viewNumber < root.view.viewNumber {
		return Search(root.Left, viewNumber, builderCommitment)
	} else if viewNumber > root.view.viewNumber {
		return Search(root.Right, viewNumber, builderCommitment)
	}
	if builderCommitment == root.view.builderCommitment {
		return &root.view
	} else if builderCommitment < root.view.builderCommitment {
		return Search(root.Left, viewNumber, builderCommitment)
	} else if builderCommitment > root.view.builderCommitment {
		return Search(root.Right, viewNumber, builderCommitment)
	}
	return nil
}

func Delete(root *ViewStoreBinaryTree, viewNumber uint64) *ViewStoreBinaryTree {
	if root == nil {
		return nil
	}
	// Find the node where this view number is located first and then delete any nodes
	// which have the view number equal or less than the node's view number
	if viewNumber < root.view.viewNumber {
		root.Left = Delete(root.Left, viewNumber)
	} else if viewNumber > root.view.viewNumber {
		root.Right = Delete(root.Right, viewNumber)
	}

	if root.Left == nil && root.Right == nil {
		return nil
	}
	return root
}
