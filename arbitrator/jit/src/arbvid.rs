// Copyright 2022, Offchain Labs, Inc.
// For license information, see https://github.com/nitro/blob/master/LICENSE

use crate::{gostack::GoStack, machine::WasmEnvMut};
use ark_bls12_381::Bls12_381;
use ark_serialize::{CanonicalDeserialize, CanonicalSerialize};
use ark_std::rand::SeedableRng;
use bincode;
use derive_more::{Display, From, Into};
use jf_primitives::pcs::prelude::UnivariateUniversalParams;
use jf_primitives::{
    pcs::{checked_fft_size, prelude::UnivariateKzgPCS, PolynomialCommitmentScheme},
    vid::advz::{payload_prover::LargeRangeProof, Advz},
};
use tagged_base64::TaggedBase64;
// use sequencer::block::entry::TxTableEntryWord;
// use sequencer::block::{payload::NamespaceProof, tables::NameSpaceTable};

pub fn verify_namespace(mut env: WasmEnvMut, sp: u32) {
    // dbg!("attempting to verify namespace");
    // let (sp, _) = GoStack::new(sp, &mut env);
    // let namespace = sp.read_u64(0);
    // let proof_buf_ptr = sp.read_u64(1);
    // let proof_buf_len = sp.read_u64(2);
    // let block_comm_buf_ptr = sp.read_u64(4);
    // let block_comm_buf_len = sp.read_u64(5);
    // let ns_table_bytes_ptr = sp.read_u64(7);
    // let ns_table_bytes_len = sp.read_u64(8);
    // let txs_buf_ptr = sp.read_u64(10);
    // let txs_buf_len = sp.read_u64(11);

    // let proof_bytes = sp.read_slice(proof_buf_ptr, proof_buf_len);
    // let block_comm_bytes = sp.read_slice(block_comm_buf_ptr, block_comm_buf_len);
    // let txs_bytes = sp.read_slice(txs_buf_ptr, txs_buf_len);
    // let ns_table_bytes = sp.read_slice(ns_table_bytes_ptr, ns_table_bytes_len);

    // dbg!("NS Table");
    // dbg!(&ns_table_bytes);
    // dbg!("Proof bytes");
    // dbg!(&proof_bytes);
    // dbg!("Block comm bytes");
    // dbg!(&block_comm_bytes);

    // let advz: Advz<Bls12_381, sha2::Sha256>;
    // let (payload_chunk_size, num_storage_nodes) = (8, 10);
    // let srs_bytes = [
    //     9, 0, 0, 0, 0, 0, 0, 0, 174, 40, 12, 59, 118, 252, 115, 21, 132, 84, 3, 147, 164, 106, 107,
    //     5, 59, 99, 162, 246, 249, 21, 195, 121, 230, 167, 129, 62, 20, 255, 151, 214, 248, 202, 38,
    //     203, 214, 90, 225, 82, 244, 127, 56, 169, 65, 92, 134, 13, 129, 230, 113, 235, 219, 70, 61,
    //     187, 113, 134, 216, 21, 0, 230, 216, 210, 132, 5, 125, 155, 16, 96, 181, 101, 159, 179,
    //     201, 202, 252, 21, 110, 41, 214, 5, 55, 42, 17, 16, 126, 106, 154, 208, 68, 75, 55, 196,
    //     205, 129, 135, 182, 211, 6, 5, 219, 148, 12, 243, 180, 103, 248, 34, 170, 170, 27, 181, 40,
    //     44, 252, 189, 8, 48, 20, 212, 107, 180, 117, 23, 85, 53, 216, 106, 91, 182, 10, 88, 19,
    //     177, 55, 42, 201, 182, 158, 90, 125, 186, 131, 165, 48, 198, 200, 116, 37, 119, 165, 142,
    //     48, 171, 149, 38, 153, 182, 232, 28, 188, 113, 104, 182, 64, 105, 217, 181, 142, 25, 246,
    //     66, 46, 47, 144, 210, 4, 140, 74, 253, 13, 59, 42, 98, 65, 236, 225, 195, 41, 156, 24, 172,
    //     232, 195, 214, 171, 105, 117, 138, 136, 186, 232, 169, 67, 236, 21, 102, 54, 93, 226, 82,
    //     194, 82, 98, 91, 98, 86, 113, 24, 169, 250, 18, 99, 236, 192, 220, 216, 220, 232, 20, 238,
    //     41, 51, 73, 219, 235, 1, 3, 64, 145, 204, 57, 91, 195, 177, 141, 161, 247, 39, 136, 31,
    //     149, 200, 222, 177, 199, 192, 227, 100, 107, 94, 214, 32, 163, 233, 201, 160, 31, 165, 136,
    //     236, 11, 141, 50, 170, 148, 91, 83, 49, 227, 129, 84, 16, 69, 23, 244, 27, 175, 47, 60, 95,
    //     74, 76, 98, 65, 104, 123, 50, 18, 54, 119, 146, 47, 206, 206, 210, 141, 138, 68, 84, 190,
    //     237, 232, 62, 121, 34, 207, 235, 195, 176, 244, 186, 75, 182, 3, 192, 254, 56, 94, 195,
    //     188, 156, 192, 195, 24, 134, 1, 78, 141, 171, 29, 151, 193, 239, 131, 86, 117, 53, 220,
    //     204, 240, 133, 126, 71, 138, 205, 23, 235, 72, 183, 213, 112, 51, 187, 218, 123, 90, 154,
    //     197, 101, 157, 179, 246, 104, 101, 104, 90, 81, 41, 59, 84, 65, 211, 184, 201, 98, 163,
    //     242, 196, 202, 131, 255, 17, 254, 74, 176, 164, 254, 0, 78, 53, 79, 95, 179, 93, 127, 216,
    //     189, 41, 42, 235, 30, 88, 12, 14, 3, 160, 180, 53, 175, 71, 129, 237, 113, 65, 192, 64,
    //     137, 254, 157, 22, 160, 10, 250, 70, 34, 39, 191, 88, 62, 112, 22, 194, 149, 187, 116, 3,
    //     36, 174, 65, 137, 54, 193, 210, 23, 40, 59, 105, 143, 21, 244, 194, 139, 31, 141, 17, 81,
    //     202, 227, 47, 103, 167, 251, 75, 139, 85, 138, 118, 212, 19, 215, 51, 181, 52, 216, 101,
    //     130, 237, 252, 43, 26, 235, 27, 128, 226, 227, 233, 34, 159, 201, 124, 22, 90, 105, 135,
    //     202, 42, 33, 92, 86, 170, 51, 144, 55, 2, 20, 75, 230, 146, 201, 162, 238, 45, 15, 95, 130,
    //     174, 153, 81, 219, 74, 193, 124, 147, 156, 121, 127, 180, 199, 84, 202, 102, 206, 130, 50,
    //     169, 201, 172, 239, 92, 224, 13, 153, 66, 183, 69, 190, 49, 128, 38, 153, 45, 108, 46, 16,
    //     214, 168, 12, 191, 81, 10, 229, 105, 1, 157, 10, 186, 215, 37, 240, 203, 189, 190, 220,
    //     196, 20, 240, 91, 179, 57, 16, 111, 101, 183, 58, 222, 80, 57, 33, 13, 154, 195, 200, 243,
    //     93, 160, 156, 216, 128, 61, 176, 103, 194, 224, 194, 52, 33, 208, 47, 198, 160, 24, 137, 2,
    //     0, 0, 0, 0, 0, 0, 0, 160, 10, 250, 70, 34, 39, 191, 88, 62, 112, 22, 194, 149, 187, 116, 3,
    //     36, 174, 65, 137, 54, 193, 210, 23, 40, 59, 105, 143, 21, 244, 194, 139, 31, 141, 17, 81,
    //     202, 227, 47, 103, 167, 251, 75, 139, 85, 138, 118, 212, 19, 215, 51, 181, 52, 216, 101,
    //     130, 237, 252, 43, 26, 235, 27, 128, 226, 227, 233, 34, 159, 201, 124, 22, 90, 105, 135,
    //     202, 42, 33, 92, 86, 170, 51, 144, 55, 2, 20, 75, 230, 146, 201, 162, 238, 45, 15, 95, 130,
    //     174, 153, 81, 219, 74, 193, 124, 147, 156, 121, 127, 180, 199, 84, 202, 102, 206, 130, 50,
    //     169, 201, 172, 239, 92, 224, 13, 153, 66, 183, 69, 190, 49, 128, 38, 153, 45, 108, 46, 16,
    //     214, 168, 12, 191, 81, 10, 229, 105, 1, 157, 10, 186, 215, 37, 240, 203, 189, 190, 220,
    //     196, 20, 240, 91, 179, 57, 16, 111, 101, 183, 58, 222, 80, 57, 33, 13, 154, 195, 200, 243,
    //     93, 160, 156, 216, 128, 61, 176, 103, 194, 224, 194, 52, 33, 208, 47, 198, 160, 24, 137,
    // ];
    // let srs_vec = srs_bytes.to_vec();
    // let srs = UnivariateUniversalParams::<Bls12_381>::deserialize_compressed(&*srs_vec).unwrap();
    // advz = Advz::new(payload_chunk_size, num_storage_nodes, 1, srs).unwrap();

    // // Deserialize here
    // let proof: NamespaceProof = bincode::deserialize(proof_bytes.as_slice()).unwrap();
    // let ns_table = NameSpaceTable::<TxTableEntryWord>::from_vec(ns_table_bytes);
    // let commit: <VidScheme as VidSchemeTrait>::Commit =
    //     bincode::deserialize(proof_bytes.as_slice()).unwrap();
    // proof.verify(&advz, &commit, &ns_table).unwrap();
}

#[test]
fn test_stuff() {
    let data = "[0, 0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0]";
    let advz: Advz<Bls12_381, sha2::Sha256>;
    let (payload_chunk_size, num_storage_nodes) = (8, 10);
    let srs_bytes = [
        9, 0, 0, 0, 0, 0, 0, 0, 174, 40, 12, 59, 118, 252, 115, 21, 132, 84, 3, 147, 164, 106, 107,
        5, 59, 99, 162, 246, 249, 21, 195, 121, 230, 167, 129, 62, 20, 255, 151, 214, 248, 202, 38,
        203, 214, 90, 225, 82, 244, 127, 56, 169, 65, 92, 134, 13, 129, 230, 113, 235, 219, 70, 61,
        187, 113, 134, 216, 21, 0, 230, 216, 210, 132, 5, 125, 155, 16, 96, 181, 101, 159, 179,
        201, 202, 252, 21, 110, 41, 214, 5, 55, 42, 17, 16, 126, 106, 154, 208, 68, 75, 55, 196,
        205, 129, 135, 182, 211, 6, 5, 219, 148, 12, 243, 180, 103, 248, 34, 170, 170, 27, 181, 40,
        44, 252, 189, 8, 48, 20, 212, 107, 180, 117, 23, 85, 53, 216, 106, 91, 182, 10, 88, 19,
        177, 55, 42, 201, 182, 158, 90, 125, 186, 131, 165, 48, 198, 200, 116, 37, 119, 165, 142,
        48, 171, 149, 38, 153, 182, 232, 28, 188, 113, 104, 182, 64, 105, 217, 181, 142, 25, 246,
        66, 46, 47, 144, 210, 4, 140, 74, 253, 13, 59, 42, 98, 65, 236, 225, 195, 41, 156, 24, 172,
        232, 195, 214, 171, 105, 117, 138, 136, 186, 232, 169, 67, 236, 21, 102, 54, 93, 226, 82,
        194, 82, 98, 91, 98, 86, 113, 24, 169, 250, 18, 99, 236, 192, 220, 216, 220, 232, 20, 238,
        41, 51, 73, 219, 235, 1, 3, 64, 145, 204, 57, 91, 195, 177, 141, 161, 247, 39, 136, 31,
        149, 200, 222, 177, 199, 192, 227, 100, 107, 94, 214, 32, 163, 233, 201, 160, 31, 165, 136,
        236, 11, 141, 50, 170, 148, 91, 83, 49, 227, 129, 84, 16, 69, 23, 244, 27, 175, 47, 60, 95,
        74, 76, 98, 65, 104, 123, 50, 18, 54, 119, 146, 47, 206, 206, 210, 141, 138, 68, 84, 190,
        237, 232, 62, 121, 34, 207, 235, 195, 176, 244, 186, 75, 182, 3, 192, 254, 56, 94, 195,
        188, 156, 192, 195, 24, 134, 1, 78, 141, 171, 29, 151, 193, 239, 131, 86, 117, 53, 220,
        204, 240, 133, 126, 71, 138, 205, 23, 235, 72, 183, 213, 112, 51, 187, 218, 123, 90, 154,
        197, 101, 157, 179, 246, 104, 101, 104, 90, 81, 41, 59, 84, 65, 211, 184, 201, 98, 163,
        242, 196, 202, 131, 255, 17, 254, 74, 176, 164, 254, 0, 78, 53, 79, 95, 179, 93, 127, 216,
        189, 41, 42, 235, 30, 88, 12, 14, 3, 160, 180, 53, 175, 71, 129, 237, 113, 65, 192, 64,
        137, 254, 157, 22, 160, 10, 250, 70, 34, 39, 191, 88, 62, 112, 22, 194, 149, 187, 116, 3,
        36, 174, 65, 137, 54, 193, 210, 23, 40, 59, 105, 143, 21, 244, 194, 139, 31, 141, 17, 81,
        202, 227, 47, 103, 167, 251, 75, 139, 85, 138, 118, 212, 19, 215, 51, 181, 52, 216, 101,
        130, 237, 252, 43, 26, 235, 27, 128, 226, 227, 233, 34, 159, 201, 124, 22, 90, 105, 135,
        202, 42, 33, 92, 86, 170, 51, 144, 55, 2, 20, 75, 230, 146, 201, 162, 238, 45, 15, 95, 130,
        174, 153, 81, 219, 74, 193, 124, 147, 156, 121, 127, 180, 199, 84, 202, 102, 206, 130, 50,
        169, 201, 172, 239, 92, 224, 13, 153, 66, 183, 69, 190, 49, 128, 38, 153, 45, 108, 46, 16,
        214, 168, 12, 191, 81, 10, 229, 105, 1, 157, 10, 186, 215, 37, 240, 203, 189, 190, 220,
        196, 20, 240, 91, 179, 57, 16, 111, 101, 183, 58, 222, 80, 57, 33, 13, 154, 195, 200, 243,
        93, 160, 156, 216, 128, 61, 176, 103, 194, 224, 194, 52, 33, 208, 47, 198, 160, 24, 137, 2,
        0, 0, 0, 0, 0, 0, 0, 160, 10, 250, 70, 34, 39, 191, 88, 62, 112, 22, 194, 149, 187, 116, 3,
        36, 174, 65, 137, 54, 193, 210, 23, 40, 59, 105, 143, 21, 244, 194, 139, 31, 141, 17, 81,
        202, 227, 47, 103, 167, 251, 75, 139, 85, 138, 118, 212, 19, 215, 51, 181, 52, 216, 101,
        130, 237, 252, 43, 26, 235, 27, 128, 226, 227, 233, 34, 159, 201, 124, 22, 90, 105, 135,
        202, 42, 33, 92, 86, 170, 51, 144, 55, 2, 20, 75, 230, 146, 201, 162, 238, 45, 15, 95, 130,
        174, 153, 81, 219, 74, 193, 124, 147, 156, 121, 127, 180, 199, 84, 202, 102, 206, 130, 50,
        169, 201, 172, 239, 92, 224, 13, 153, 66, 183, 69, 190, 49, 128, 38, 153, 45, 108, 46, 16,
        214, 168, 12, 191, 81, 10, 229, 105, 1, 157, 10, 186, 215, 37, 240, 203, 189, 190, 220,
        196, 20, 240, 91, 179, 57, 16, 111, 101, 183, 58, 222, 80, 57, 33, 13, 154, 195, 200, 243,
        93, 160, 156, 216, 128, 61, 176, 103, 194, 224, 194, 52, 33, 208, 47, 198, 160, 24, 137,
    ];
    let srs_vec = srs_bytes.to_vec();
    let srs = UnivariateUniversalParams::<Bls12_381>::deserialize_compressed(&*srs_vec).unwrap();
    advz = Advz::new(payload_chunk_size, num_storage_nodes, 1, srs).unwrap();
    let ns_table = NameSpaceTable::<TxTableEntryWord>::from_vec(Vec::new());
    let proof: NamespaceProof = serde_json::from_str(&"{\"NonExistence\":{\"ns_id\":0}}").unwrap();
    let tagged = TaggedBase64::parse(&"HASH~1yS-KEtL3oDZDBJdsW51Pd7zywIiHesBZsTbpOzrxOfu").unwrap();
    let commit: <VidScheme as VidSchemeTrait>::Commit = tagged.try_into().unwrap();
    proof.verify(&advz, &commit, &ns_table).unwrap();
}

// ALL OF THIS SHOULD GO IN ANOTHER REPO
use core::fmt;
use serde::{Deserialize, Serialize};
use std::mem::size_of;
use trait_set::trait_set;

trait_set! {
    pub trait TableWordTraits = CanonicalSerialize
        + CanonicalDeserialize
        + TryFrom<usize>
        + TryInto<usize>
        + Default
         + PrimInt
        + std::marker::Sync;

    // Note: this trait is not used yet as for now the Payload structs are only parametrized with the TableWord parameter.
    pub trait OffsetTraits = CanonicalSerialize
        + CanonicalDeserialize
        + TryFrom<usize>
        + TryInto<usize>
        + Default
        + std::marker::Sync;

    // Note: this trait is not used yet as for now the Payload structs are only parametrized with the TableWord parameter.
    pub trait NsIdTraits =CanonicalSerialize + CanonicalDeserialize + Default + std::marker::Sync;
}

#[derive(
    Clone,
    Copy,
    Serialize,
    Deserialize,
    Debug,
    Display,
    PartialEq,
    Eq,
    Hash,
    Into,
    From,
    Default,
    CanonicalDeserialize,
    CanonicalSerialize,
    PartialOrd,
    Ord,
)]
pub struct VmId(pub(crate) u64);

// Use newtype pattern so that tx table entires cannot be confused with other types.
#[derive(Clone, Debug, Deserialize, Eq, Hash, PartialEq, Serialize, Default)]
pub struct TxTableEntry(TxTableEntryWord);
// TODO Get rid of TxTableEntryWord. We might use const generics in order to parametrize the set of functions below with u32,u64  etc...
// See https://github.com/EspressoSystems/espresso-sequencer/issues/1076
pub type TxTableEntryWord = u32;

pub struct TxTable {}
impl TxTable {
    // Parse `TxTableEntry::byte_len()`` bytes from `raw_payload`` starting at `offset` into a `TxTableEntry`
    pub(crate) fn get_len(raw_payload: &[u8], offset: usize) -> TxTableEntry {
        let end = std::cmp::min(
            offset.saturating_add(TxTableEntry::byte_len()),
            raw_payload.len(),
        );
        let start = std::cmp::min(offset, end);
        let tx_table_len_range = start..end;
        let mut entry_bytes = [0u8; TxTableEntry::byte_len()];
        entry_bytes[..tx_table_len_range.len()].copy_from_slice(&raw_payload[tx_table_len_range]);
        TxTableEntry::from_bytes_array(entry_bytes)
    }

    // Parse the table length from the beginning of the tx table inside `ns_bytes`.
    //
    // Returned value is guaranteed to be no larger than the number of tx table entries that could possibly fit into `ns_bytes`.
    // TODO tidy this is a sloppy wrapper for get_len
    pub(crate) fn get_tx_table_len(ns_bytes: &[u8]) -> usize {
        std::cmp::min(
            Self::get_len(ns_bytes, 0).try_into().unwrap_or(0),
            (ns_bytes.len().saturating_sub(TxTableEntry::byte_len())) / TxTableEntry::byte_len(),
        )
    }

    // returns tx_offset
    // if tx_index would reach beyond ns_bytes then return 0.
    // tx_offset is not checked, could be anything
    pub(crate) fn get_table_entry(ns_bytes: &[u8], tx_index: usize) -> usize {
        // get the range for tx_offset bytes in tx table
        let tx_offset_range = {
            let start = std::cmp::min(
                tx_index
                    .saturating_add(1)
                    .saturating_mul(TxTableEntry::byte_len()),
                ns_bytes.len(),
            );
            let end = std::cmp::min(
                start.saturating_add(TxTableEntry::byte_len()),
                ns_bytes.len(),
            );
            start..end
        };

        // parse tx_offset bytes from tx table
        let mut tx_offset_bytes = [0u8; TxTableEntry::byte_len()];
        tx_offset_bytes[..tx_offset_range.len()].copy_from_slice(&ns_bytes[tx_offset_range]);
        usize::try_from(TxTableEntry::from_bytes(&tx_offset_bytes).unwrap_or(TxTableEntry::zero()))
            .unwrap_or(0)
    }
}

impl TxTableEntry {
    pub const MAX: TxTableEntry = Self(TxTableEntryWord::MAX);

    /// Adds `rhs` to `self` in place. Returns `None` on overflow.
    pub fn checked_add_mut(&mut self, rhs: Self) -> Option<()> {
        self.0 = self.0.checked_add(rhs.0)?;
        Some(())
    }
    pub const fn zero() -> Self {
        Self(0)
    }
    pub const fn one() -> Self {
        Self(1)
    }
    pub const fn to_bytes(&self) -> [u8; size_of::<TxTableEntryWord>()] {
        self.0.to_le_bytes()
    }
    pub fn from_bytes(bytes: &[u8]) -> Option<Self> {
        Some(Self(TxTableEntryWord::from_le_bytes(
            bytes.try_into().ok()?,
        )))
    }
    /// Infallible constructor.
    pub fn from_bytes_array(bytes: [u8; TxTableEntry::byte_len()]) -> Self {
        Self(TxTableEntryWord::from_le_bytes(bytes))
    }
    pub const fn byte_len() -> usize {
        size_of::<TxTableEntryWord>()
    }

    pub fn from_usize(val: usize) -> Self {
        Self(
            val.try_into()
                .expect("usize -> TxTableEntry should succeed"),
        )
    }
}

impl fmt::Display for TxTableEntry {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        write!(f, "{}", self.0)
    }
}

impl TryFrom<usize> for TxTableEntry {
    type Error = <TxTableEntryWord as TryFrom<usize>>::Error;

    fn try_from(value: usize) -> Result<Self, Self::Error> {
        TxTableEntryWord::try_from(value).map(Self)
    }
}
impl TryFrom<TxTableEntry> for usize {
    type Error = <usize as TryFrom<TxTableEntryWord>>::Error;

    fn try_from(value: TxTableEntry) -> Result<Self, Self::Error> {
        usize::try_from(value.0)
    }
}

impl TryFrom<VmId> for TxTableEntry {
    type Error = <TxTableEntryWord as TryFrom<u64>>::Error;

    fn try_from(value: VmId) -> Result<Self, Self::Error> {
        TxTableEntryWord::try_from(value.0).map(Self)
    }
}
impl TryFrom<TxTableEntry> for VmId {
    type Error = <u64 as TryFrom<TxTableEntryWord>>::Error;

    fn try_from(value: TxTableEntry) -> Result<Self, Self::Error> {
        Ok(Self(From::from(value.0)))
    }
}

// use crate::block::entry::{TxTableEntry, TxTableEntryWord};
// use crate::block::payload;
// use crate::{BlockBuildingSnafu, Error, Transaction, VmId};
// use derivative::Derivative;
use derivative::Derivative;
use jf_primitives::vid::{
    advz::payload_prover::SmallRangeProof,
    payload_prover::{PayloadProver, Statement},
    VidScheme as VidSchemeTrait,
};
use num_traits::PrimInt;
// use snafu::OptionExt;
use std::default::Default;
use std::sync::OnceLock;
use std::{collections::HashMap, fmt::Display, marker::PhantomData, ops::Range};

pub type VidScheme = jf_primitives::vid::advz::Advz<ark_bls12_381::Bls12_381, sha2::Sha256>;

// use crate::block::tables::NameSpaceTable;
// use trait_set::trait_set;

// use super::tables::TxTable;

// trait_set! {

//     pub trait TableWordTraits = CanonicalSerialize
//         + CanonicalDeserialize
//         + TryFrom<usize>
//         + TryInto<usize>
//         + Default
//          + PrimInt
//         + std::marker::Sync;

//     // Note: this trait is not used yet as for now the Payload structs are only parametrized with the TableWord parameter.
//     pub trait OffsetTraits = CanonicalSerialize
//         + CanonicalDeserialize
//         + TryFrom<usize>
//         + TryInto<usize>
//         + Default
//         + std::marker::Sync;

//     // Note: this trait is not used yet as for now the Payload structs are only parametrized with the TableWord parameter.
//     pub trait NsIdTraits =CanonicalSerialize + CanonicalDeserialize + Default + std::marker::Sync;
// }

/// Namespace proof type
///
/// # Type complexity
///
/// Jellyfish's `LargeRangeProof` type has a prime field generic parameter `F`.
/// This `F` is determined by the pairing parameter for `Advz` currently returned by `test_vid_factory()`.
/// Jellyfish needs a more ergonomic way for downstream users to refer to this type.
///
/// There is a `KzgEval` type alias in jellyfish that helps a little, but it's currently private.
/// If it were public then we could instead use
/// ```compile_fail
/// LargeRangeProof<KzgEval<Bls12_281>>
/// ```
/// but that's still pretty crufty.
pub type JellyfishNamespaceProof =
    LargeRangeProof<<UnivariateKzgPCS<Bls12_381> as PolynomialCommitmentScheme>::Evaluation>;

#[derive(Clone, Debug, Eq, PartialEq, Serialize, Deserialize)]
#[serde(bound = "")] // for V
pub enum NamespaceProof {
    Existence {
        ns_payload_flat: Vec<u8>,
        ns_id: VmId,
        ns_proof: JellyfishNamespaceProof,
        vid_common: <VidScheme as VidSchemeTrait>::Common,
    },
    NonExistence {
        ns_id: VmId,
    },
}

impl NamespaceProof {
    /// Verify a [`NamespaceProof`].
    ///
    /// All args must be available to the verifier in the block header.
    #[allow(dead_code)] // TODO temporary
    pub fn verify(
        &self,
        vid: &VidScheme,
        commit: &<VidScheme as VidSchemeTrait>::Commit,
        ns_table: &NameSpaceTable<TxTableEntryWord>,
    ) -> Option<(Vec<Transaction>, VmId)> {
        match self {
            NamespaceProof::Existence {
                ns_payload_flat,
                ns_id,
                ns_proof,
                vid_common,
            } => {
                let ns_index = ns_table.lookup(*ns_id)?;

                // TODO rework NameSpaceTable struct
                // TODO merge get_ns_payload_range with get_ns_table_entry ?
                let ns_payload_range = ns_table
                    .get_payload_range(ns_index, VidScheme::get_payload_byte_len(vid_common));

                let ns_id = ns_table.get_table_entry(ns_index).0;

                // verify self against args
                vid.payload_verify(
                    Statement {
                        payload_subslice: ns_payload_flat,
                        range: ns_payload_range,
                        commit,
                        common: vid_common,
                    },
                    ns_proof,
                )
                .ok()?
                .ok()?;

                // verification succeeded, return some data
                // we know ns_id is correct because the corresponding ns_payload_range passed verification
                Some((parse_ns_payload(ns_payload_flat, ns_id), ns_id))
            }
            NamespaceProof::NonExistence { ns_id } => {
                if ns_table.lookup(*ns_id).is_some() {
                    return None; // error: expect not to find ns_id in ns_table
                }
                Some((Vec::new(), *ns_id))
            }
        }
    }
}

pub struct Transaction {
    vm: VmId,
    payload: Vec<u8>,
}

impl Transaction {
    pub fn new(vm: VmId, payload: Vec<u8>) -> Self {
        Self { vm, payload }
    }
}

// TODO find a home for this function
pub fn parse_ns_payload(ns_payload_flat: &[u8], ns_id: VmId) -> Vec<Transaction> {
    let num_txs = TxTable::get_tx_table_len(ns_payload_flat);
    let tx_bodies_offset = num_txs
        .saturating_add(1)
        .saturating_mul(TxTableEntry::byte_len());
    let mut txs = Vec::with_capacity(num_txs);
    let mut start = tx_bodies_offset;
    for tx_index in 0..num_txs {
        let end = std::cmp::min(
            TxTable::get_table_entry(ns_payload_flat, tx_index).saturating_add(tx_bodies_offset),
            ns_payload_flat.len(),
        );
        let tx_payload_range = Range {
            start: std::cmp::min(start, end),
            end,
        };
        txs.push(Transaction::new(
            ns_id,
            ns_payload_flat[tx_payload_range].to_vec(),
        ));
        start = end;
    }

    txs
}

#[derive(Clone, Debug, Derivative, Deserialize, Eq, Serialize, Default)]
#[derivative(Hash, PartialEq)]
pub struct NameSpaceTable<TableWord: TableWordTraits> {
    pub bytes: Vec<u8>,
    pub phantom: PhantomData<TableWord>,
}

impl<TableWord: TableWordTraits> NameSpaceTable<TableWord> {
    pub fn from_vec(v: Vec<u8>) -> Self {
        Self {
            bytes: v,
            phantom: Default::default(),
        }
    }

    /// Find `ns_id` and return its index into this namespace table.
    ///
    /// TODO return Result or Option? Want to avoid catch-all Error type :(
    pub fn lookup(&self, ns_id: VmId) -> Option<usize> {
        // TODO don't use TxTable, need a new method
        let ns_table_len = TxTable::get_tx_table_len(&self.bytes);

        (0..ns_table_len).find(|&ns_index| ns_id == self.get_table_entry(ns_index).0)
    }

    // Parse the table length from the beginning of the namespace table.
    // Returned value is guaranteed to be no larger than the number of ns table entries that could possibly fit into `ns_table_bytes`.
    pub fn len(&self) -> usize {
        let left = self.get_table_len(0).try_into().unwrap_or(0);
        let right = (self.bytes.len() - TxTableEntry::byte_len()) / (2 * TxTableEntry::byte_len());
        std::cmp::min(left, right)
    }

    // returns (ns_id, ns_offset)
    // ns_offset is not checked, could be anything
    pub fn get_table_entry(&self, ns_index: usize) -> (VmId, usize) {
        // get the range for ns_id bytes in ns table
        // ensure `range` is within range for ns_table_bytes
        let start = std::cmp::min(
            ns_index
                .saturating_mul(2)
                .saturating_add(1)
                .saturating_mul(TxTableEntry::byte_len()),
            self.bytes.len(),
        );
        let end = std::cmp::min(
            start.saturating_add(TxTableEntry::byte_len()),
            self.bytes.len(),
        );
        let ns_id_range = start..end;

        // parse ns_id bytes from ns table
        // any failure -> VmId(0)
        let mut ns_id_bytes = [0u8; TxTableEntry::byte_len()];
        ns_id_bytes[..ns_id_range.len()].copy_from_slice(&self.bytes[ns_id_range]);
        let ns_id =
            VmId::try_from(TxTableEntry::from_bytes(&ns_id_bytes).unwrap_or(TxTableEntry::zero()))
                .unwrap_or(VmId(0));

        // get the range for ns_offset bytes in ns table
        // ensure `range` is within range for ns_table_bytes
        // TODO refactor range checking code
        let start = end;
        let end = std::cmp::min(
            start.saturating_add(TxTableEntry::byte_len()),
            self.bytes.len(),
        );
        let ns_offset_range = start..end;

        // parse ns_offset bytes from ns table
        // any failure -> 0 offset (?)
        // TODO refactor parsing code?
        let mut ns_offset_bytes = [0u8; TxTableEntry::byte_len()];
        ns_offset_bytes[..ns_offset_range.len()].copy_from_slice(&self.bytes[ns_offset_range]);
        let ns_offset = usize::try_from(
            TxTableEntry::from_bytes(&ns_offset_bytes).unwrap_or(TxTableEntry::zero()),
        )
        .unwrap_or(0);

        (ns_id, ns_offset)
    }

    /// Like `tx_payload_range` except for namespaces.
    /// Returns the byte range for a ns in the block payload bytes.
    ///
    /// Ensures that the returned range is valid: `start <= end <= block_payload_byte_len`.
    pub fn get_payload_range(
        &self,
        ns_index: usize,
        block_payload_byte_len: usize,
    ) -> Range<usize> {
        let end = std::cmp::min(self.get_table_entry(ns_index).1, block_payload_byte_len);
        let start = if ns_index == 0 {
            0
        } else {
            std::cmp::min(self.get_table_entry(ns_index - 1).1, end)
        };
        start..end
    }
}

pub trait Table<TableWord: TableWordTraits> {
    // Read TxTableEntry::byte_len() bytes from `table_bytes` starting at `offset`.
    // if `table_bytes` has too few bytes at this `offset` then pad with zero.
    // Parse these bytes into a `TxTableEntry` and return.
    // Returns raw bytes, no checking for large values
    fn get_table_len(&self, offset: usize) -> TxTableEntry;

    fn get_payload(&self) -> Vec<u8>;

    fn byte_len() -> usize {
        size_of::<TableWord>()
    }
}

impl<TableWord: TableWordTraits> Table<TableWord> for NameSpaceTable<TableWord> {
    // TODO (Philippe) avoid code duplication with similar function in TxTable?
    fn get_table_len(&self, offset: usize) -> TxTableEntry {
        let end = std::cmp::min(
            offset.saturating_add(TxTableEntry::byte_len()),
            self.bytes.len(),
        );
        let start = std::cmp::min(offset, end);
        let tx_table_len_range = start..end;
        let mut entry_bytes = [0u8; TxTableEntry::byte_len()];
        entry_bytes[..tx_table_len_range.len()].copy_from_slice(&self.bytes[tx_table_len_range]);
        TxTableEntry::from_bytes_array(entry_bytes)
    }

    fn get_payload(&self) -> Vec<u8> {
        self.bytes.clone()
    }
}
