mod bytes;
mod full_payload;
mod hotshot_types;
mod namespace_payload;
mod sequencer_data_structures;
mod uint_bytes;
mod utils;

use ark_ff::PrimeField;
use ark_serialize::{CanonicalDeserialize, CanonicalSerialize};
use committable::{Commitment, Committable};
use ethers_core::types::U256;
use full_payload::{NsProof, NsTable};
use hotshot_types::{VidCommitment, VidCommon};
use jf_crhf::CRHF;
use jf_merkle_tree::prelude::{
    MerkleCommitment, MerkleNode, MerkleProof, MerkleTreeScheme, Sha3Node,
};
use jf_rescue::{crhf::VariableLengthRescueCRHF, RescueError};
use sequencer_data_structures::{
    field_to_u256, BlockMerkleCommitment, BlockMerkleTree, Header, Transaction,
};
use sha2::{Digest, Sha256};
use std::{
    ffi::{
        CStr,
        c_char,
    }
};
use tagged_base64::TaggedBase64;

#[derive(
    Clone,
    Copy,
    Debug,
    PartialEq,
    Eq,
    Hash,
    Default,
    CanonicalDeserialize,
    CanonicalSerialize,
    PartialOrd,
    Ord,
)]
pub struct NamespaceId(u64);

impl From<NamespaceId> for u32 {
    fn from(value: NamespaceId) -> Self {
        value.0 as Self
    }
}

impl From<u32> for NamespaceId {
    fn from(value: u32) -> Self {
        Self(value as u64)
    }
}

// pub type VidScheme = Advz<Bn254, sha2::Sha256>;
pub type Proof = Vec<MerkleNode<Commitment<Header>, u64, Sha3Node>>;
pub type CircuitField = ark_ed_on_bn254::Fq;

// Helper function to verify a block merkle proof.
// For the purposes of FFI with go, the parameters in to this function must be of type const* c_char
// proof_bytes: Byte representation of a block merkle proof.
// root_bytes: Byte representation of a Sha3Node merkle root.
// header_bytes: Byte representation of the HotShot header being validated as a Merkle leaf.
// circuit_block_bytes: Circuit representation of the HotShot header commitment returned by the light client contract.
#[no_mangle]
pub extern "C" fn verify_merkle_proof_helper(
    proof_c_bytes: *const c_char,
    header_c_bytes: *const c_char,
    block_comm_c_bytes: *const c_char,
    circuit_block_c_bytes: *const c_char,
) -> bool {
    let proof_c_str = unsafe { CStr::from_ptr(proof_c_bytes) };

    let header_c_str = unsafe { CStr::from_ptr(header_c_bytes) };

    let block_comm_c_str = unsafe { CStr::from_ptr(block_comm_c_bytes) };

    let circuit_block_c_str = unsafe { CStr::from_ptr(circuit_block_c_bytes) };

    let proof_bytes = proof_c_str.to_bytes();
    let header_bytes = header_c_str.to_bytes();
    let block_comm_bytes = block_comm_c_str.to_bytes();
    let circuit_block_bytes = circuit_block_c_str.to_bytes();

    let proof_str = std::str::from_utf8(proof_bytes).unwrap();
    let header_str = std::str::from_utf8(header_bytes).unwrap();
    let block_comm_str = std::str::from_utf8(block_comm_bytes).unwrap();
    let tagged = TaggedBase64::parse(&block_comm_str).unwrap();
    let block_comm: BlockMerkleCommitment = tagged.try_into().unwrap();

    let proof: Proof = serde_json::from_str(proof_str).unwrap();
    let header: Header = serde_json::from_str(header_str).unwrap();
    let header_comm: Commitment<Header> = header.commit();

    let proof = MerkleProof::new(header.height, proof.to_vec());
    let proved_comm = proof.elem().unwrap().clone();
    BlockMerkleTree::verify(block_comm.digest(), header.height, proof)
        .unwrap()
        .unwrap();

    let mut block_comm_root_bytes = vec![];
    block_comm
        .serialize_compressed(&mut block_comm_root_bytes)
        .unwrap();
    let field_bytes = hash_bytes_to_field(&block_comm_root_bytes).unwrap();
    let local_block_comm_u256 = field_to_u256(field_bytes);
    let circuit_block_comm_u256 = U256::from_little_endian(circuit_block_bytes);

    //assert!(proved_comm == header_comm);
    //assert!(local_block_comm_u256 == circuit_block_comm_u256);

    if (proved_comm == header_comm) && (local_block_comm_u256 == circuit_block_comm_u256) {
        return true;
    }
    return false;
}

// Helper function to verify a VID namespace proof that takes the byte representations of the proof,
// namespace table, and commitment string.
//
// proof_bytes: Byte representation of a JSON NamespaceProof string.
// commit_bytes: Byte representation of a TaggedBase64 payload commitment string.
// ns_table_bytes: Raw bytes of the namespace table.
// tx_comm_bytes: Byte representation of a hex encoded Sha256 digest that the transaction set commits to.
#[no_mangle]
pub extern "C" fn verify_namespace_helper(
    namespace: u64,
    proof_c_bytes: *const c_char,
    commit_c_bytes: *const c_char,
    ns_table_c_bytes: *const c_char,
    tx_comm_c_bytes: *const c_char,
    common_data_c_bytes: *const c_char,
) -> bool {
    let ns_table_c_str = unsafe { CStr::from_ptr(ns_table_c_bytes) };

    let proof_c_str = unsafe { CStr::from_ptr(proof_c_bytes) };
    let commit_c_str = unsafe { CStr::from_ptr(commit_c_bytes) };
    let tx_comm_c_str = unsafe { CStr::from_ptr(tx_comm_c_bytes) };

    let common_data_c_str = unsafe { CStr::from_ptr(common_data_c_bytes) };

    let ns_table_bytes = ns_table_c_str.to_bytes();
    let proof_bytes = proof_c_str.to_bytes();
    let commit_bytes = commit_c_str.to_bytes();
    let tx_comm_bytes = tx_comm_c_str.to_bytes();
    let common_data_bytes = common_data_c_str.to_bytes();

    let proof_str = std::str::from_utf8(proof_bytes).unwrap();
    let commit_str = std::str::from_utf8(commit_bytes).unwrap();
    let txn_comm_str = std::str::from_utf8(tx_comm_bytes).unwrap();
    let common_data_str = std::str::from_utf8(common_data_bytes).unwrap();

    let proof: NsProof = serde_json::from_str(proof_str).unwrap();
    let ns_table: NsTable = NsTable {
        bytes: ns_table_bytes.to_vec(),
    };
    let tagged = TaggedBase64::parse(&commit_str).unwrap();
    let commit: VidCommitment = tagged.try_into().unwrap();
    let vid_common: VidCommon = serde_json::from_str(common_data_str).unwrap();

    let (txns, ns) = proof.verify(&ns_table, &commit, &vid_common).unwrap();

    let namespace: u32 = namespace.try_into().unwrap();
    let txns_comm = hash_txns(namespace, &txns);

    //assert!(ns == namespace.into());
    //assert!(txns_comm == txn_comm_str);
    if (ns == namespace.into()) && (txns_comm == txn_comm_str) {
        return true;
    }
    return false;
}

// TODO: Use Commit trait: https://github.com/EspressoSystems/nitro-espresso-integration/issues/88
fn hash_txns(namespace: u32, txns: &[Transaction]) -> String {
    let mut hasher = Sha256::new();
    hasher.update(namespace.to_le_bytes());
    for txn in txns {
        hasher.update(&txn.payload);
    }
    let hash_result = hasher.finalize();
    format!("{:x}", hash_result)
}

fn hash_bytes_to_field(bytes: &[u8]) -> Result<CircuitField, RescueError> {
    // make sure that `mod_order` won't happen.
    let bytes_len = ((<CircuitField as PrimeField>::MODULUS_BIT_SIZE + 7) / 8 - 1) as usize;
    let elem = bytes
        .chunks(bytes_len)
        .map(CircuitField::from_le_bytes_mod_order)
        .collect::<Vec<_>>();
    Ok(VariableLengthRescueCRHF::<_, 1>::evaluate(elem)?[0])
}
