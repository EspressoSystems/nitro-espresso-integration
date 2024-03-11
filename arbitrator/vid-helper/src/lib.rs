mod namespace;

use ark_bls12_381::Bls12_381;
use ark_serialize::CanonicalDeserialize;
use jf_primitives::{
    pcs::prelude::UnivariateUniversalParams,
    vid::{advz::Advz, VidScheme as VidSchemeTrait},
};
use lazy_static::lazy_static;
use namespace::{NameSpaceTable, NamespaceProof, Transaction, TxTableEntryWord};
use sha2::{Digest, Sha256};
use tagged_base64::TaggedBase64;

pub type VidScheme = Advz<Bls12_381, sha2::Sha256>;

lazy_static! {
    // Initialize the byte array from JSON content
    static ref SRS_VEC: Vec<u8> = {
        let json_content = include_str!("../../../config/vid_srs.json");
        serde_json::from_str(json_content).expect("Failed to deserialize")
    };
}

// Helper function to verify a VID namespace proof that takes the byte representations of the proof,
// namespace table, and commitment string.
//
// proof_bytes: Byte representation of a JSON NamespaceProof string
// commit_bytes: Byte representation of a TaggedBase64 payload commitment string
// ns_table_bytes: Raw bytes of the namespace table
// tx_comm_bytes: Byte representation of a hex encoded Sha256 digest that the transaction set commits to
pub fn verify_namespace_helper(
    namespace: u64,
    proof_bytes: &[u8],
    commit_bytes: &[u8],
    ns_table_bytes: &[u8],
    tx_comm_bytes: &[u8],
) {
    let proof_str = std::str::from_utf8(proof_bytes).unwrap();
    let commit_str = std::str::from_utf8(commit_bytes).unwrap();
    let txn_comm_str = std::str::from_utf8(tx_comm_bytes).unwrap();
    println!("{:?}", namespace);
    println!("{:?}", proof_str);
    println!("{:?}", commit_str);
    println!("{:?}", txn_comm_str);
    println!("{:?}", ns_table_bytes);

    let proof: NamespaceProof = serde_json::from_str(proof_str).unwrap();
    let ns_table = NameSpaceTable::<TxTableEntryWord>::from_vec(ns_table_bytes.to_vec());
    let tagged = TaggedBase64::parse(&commit_str).unwrap();
    let commit: <VidScheme as VidSchemeTrait>::Commit = tagged.try_into().unwrap();
    println!("vid helper 1");

    let srs = UnivariateUniversalParams::<Bls12_381>::deserialize_uncompressed_unchecked(
        SRS_VEC.as_slice(),
    )
    .unwrap();
    let num_storage_nodes = match &proof {
        NamespaceProof::Existence { vid_common, .. } => {
            VidScheme::get_num_storage_nodes(&vid_common)
        }
        // Non-existence proofs do not actually make use of the SRS, so pick some random value to appease the compiler.
        _ => 5,
    };
    println!("vid helper 2");
    let num_chunks: usize = 1 << num_storage_nodes.ilog2();
    let advz = Advz::new(num_chunks, num_storage_nodes, srs).unwrap();
    println!("vid helper here");
    let (txns, ns) = proof.verify(&advz, &commit, &ns_table).unwrap();

    let txns_comm = hash_txns(namespace, &txns);
    println!("vid helper 3");

    assert!(ns == namespace.into());
    assert!(txns_comm == txn_comm_str);
}

// TODO: Use Commit trait: https://github.com/EspressoSystems/nitro-espresso-integration/issues/88
fn hash_txns(namespace: u64, txns: &[Transaction]) -> String {
    let mut hasher = Sha256::new();
    hasher.update(namespace.to_le_bytes());
    for txn in txns {
        hasher.update(&txn.payload);
    }
    let hash_result = hasher.finalize();
    format!("{:x}", hash_result)
}

#[test]
fn test_verify_namespace_helper() {
    let proof_bytes = b"{\"NonExistence\":{\"ns_id\":0}}";
    let commit_bytes = b"HASH~1yS-KEtL3oDZDBJdsW51Pd7zywIiHesBZsTbpOzrxOfu";
    let txn_comm_str = hash_txns(0, &[]);
    let txn_comm_bytes = txn_comm_str.as_bytes();
    let ns_table_bytes = &[0, 0, 0, 0];
    verify_namespace_helper(0, proof_bytes, commit_bytes, ns_table_bytes, txn_comm_bytes);
}

#[test]
fn test_verify_namespace_helper_case_1() {
    let namespace = 412346;
    let proof_bytes = b"{\"Existence\":{\"ns_payload_flat\":[1,0,0,0,117,0,0,0,2,248,114,131,6,74,186,128,128,132,11,235,194,0,132,1,201,195,128,148,7,98,9,41,180,192,252,72,6,12,79,175,55,139,30,133,104,28,157,225,135,35,134,242,111,193,0,0,128,192,128,160,57,26,105,137,113,169,5,164,123,174,246,117,101,30,187,32,82,244,164,126,250,137,214,77,232,181,137,132,247,119,251,18,160,83,160,191,50,242,171,94,74,37,10,109,120,213,144,117,86,88,16,78,161,248,135,151,213,228,243,144,208,204,36,163,178],\"ns_id\":412346,\"ns_proof\":{\"prefix_elems\":\"FIELD~AAAAAAAAAAD7\",\"suffix_elems\":\"FIELD~AAAAAAAAAAD7\",\"prefix_bytes\":[],\"suffix_bytes\":[]},\"vid_common\":{\"poly_commits\":\"FIELD~AwAAAAAAAACrEgXPu8C7P9Wjj8VkueiCK-BKbCDceNq3ylqp9E5Qr3xlPuKyonVTYLU0guykcCWZwU5UZpYXa5u3U6hvlUFEinsE7l7iuWWbKI1cZmmKXqzi52ZE56fxlTEncT2xM0C0tHaBjuFbHWlxYMBSM_KUI2fl2DPqrMtcALgKVPStlzasNjejNfNRDWPsdEAGNMZ_\",\"all_evals_digest\":\"FIELD~N78nWb9X7c2ADT7KCNwnP3W37ZDt94r0Njon0WlFeHLu\",\"payload_byte_len\":125,\"num_storage_nodes\":2,\"multiplicity\":1}}}";
    let commit_bytes = b"HASH~vH8ENqvQBTekfCUOVFqdgbyTbsxveVVuH8u7S812t-lW";
    let tx_commit_bytes = b"7e16516c7fdefd171c5fd8a40bd8bd9ce83f1fa93b870a26610e69c9d61a5775";
    let ns_table_bytes = [1, 0, 0, 0, 186, 74, 6, 0, 125, 0, 0, 0];
    verify_namespace_helper(
        namespace,
        proof_bytes,
        commit_bytes,
        &ns_table_bytes,
        tx_commit_bytes,
    );
}
