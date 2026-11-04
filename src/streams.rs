//! Stream ids. Every byte of the input lands in exactly one
//! stream: a lazy structural byte stream (coded with a side context, decoded
//! one symbol at a time by the walker), a blob stream (decoded up front,
//! read through the command sizer), or a numeric stream (residuals coded by
//! the numeric groups after the walk).

pub const TIME: usize = 0;
pub const TYPE: usize = 1;
pub const MASK: usize = 2;
pub const CMD: usize = 3;
pub const ESCLEN: usize = 4;
pub const HIDTAG: usize = 5;
pub const HIDLEN: usize = 6;
pub const HIDTYPE: usize = 7;
pub const HIDDMG: usize = 8;
pub const PI_NUM: usize = 9;
pub const PI_FLAGS: usize = 10;
pub const PI_FRAME: usize = 11;
pub const PI_MODEL: usize = 12;
pub const PI_SKIN: usize = 13;
pub const PI_EFF: usize = 14;
pub const PI_WFRAME: usize = 15;
pub const PE_HDR: usize = 16;
pub const PE_ENT: usize = 17;
pub const PE_BITS: usize = 18;
pub const PE_MORE: usize = 19;
pub const PE_MODEL: usize = 20;
pub const PE_FRAME: usize = 21;
pub const PE_MISC: usize = 22;
pub const PE_A: usize = 23;
pub const ST_IDX: usize = 24;
pub const ST_VAL: usize = 25;
pub const STL_IDX: usize = 26;
pub const SND_CH: usize = 27;
pub const SND_MISC: usize = 28;
pub const SND_NUM: usize = 29;
pub const TE_TYPE: usize = 30;
pub const TE_MISC: usize = 31;
pub const PR_LEVEL: usize = 32;
pub const FRAGS: usize = 33;
/// usercmd hidden blocks: every field but the angles on the grid
pub const UC: usize = 34;
/// The fewest UC symbols one usercmd block codes (its angles all on the
/// grid): player, dropnum, msec, grid, three moves of two bytes, buttons,
/// impulse. Bounds the usercmd record list (FORMAT.md §6.3).
pub const UC_MIN_SYMS: usize = 12;
pub const N_LAZY: usize = 35;

// numeric streams
pub const NUM_BASE: usize = 48;
pub const PI_OX: usize = 48;
pub const PI_AP: usize = 51;
pub const PE_OX: usize = 54;
pub const SND_O: usize = 57;
pub const TE_O: usize = 58;
pub const STL_VAL: usize = 59;
pub const PI_F0: usize = 60;
pub const PE_F0: usize = 63;
/// usercmd angles on the 360/65536 grid
pub const UC_A: usize = 66;
pub const N_NUM_END: usize = 67;

// blob streams
pub const RAW: usize = 80;
pub const TAIL: usize = 81;
pub const SET: usize = 82;
pub const TEXT: usize = 83;
pub const PING: usize = 84;
pub const NAILS: usize = 85;
pub const HIDMISC: usize = 86;
pub const SNDF: usize = 87;
pub const TEF: usize = 88;
/// Not a stored stream: the segment's share of the metadata's demoinfo
/// store, `u32le len | len bytes` per KTX demoinfo block.
pub const DINFO: usize = 79;
pub const GEN_BASE: usize = 128;
pub const N_STREAMS: usize = GEN_BASE + 256;

#[inline]
pub fn is_lazy(id: usize) -> bool {
    id < N_LAZY
}
#[inline]
pub fn is_num(id: usize) -> bool {
    (NUM_BASE..N_NUM_END).contains(&id)
}
#[inline]
pub fn is_blob(id: usize) -> bool {
    (RAW..N_STREAMS).contains(&id) && !(TEF + 1..GEN_BASE).contains(&id)
}

pub fn name(id: usize) -> String {
    const LAZY: [&str; N_LAZY] = [
        "time",
        "type",
        "mask",
        "cmd",
        "esclen",
        "hid.tag",
        "hid.len",
        "hid.type",
        "hid.dmg",
        "pi.num",
        "pi.flags",
        "pi.frame",
        "pi.model",
        "pi.skin",
        "pi.eff",
        "pi.wframe",
        "pe.hdr",
        "pe.ent",
        "pe.bits",
        "pe.more",
        "pe.model",
        "pe.frame",
        "pe.misc",
        "pe.a",
        "st.idx",
        "st.val",
        "stl.idx",
        "snd.ch",
        "snd.misc",
        "snd.num",
        "te.type",
        "te.misc",
        "pr.level",
        "frags",
        "uc",
    ];
    const NUM: [&str; N_NUM_END - NUM_BASE] = [
        "pi.ox", "pi.oy", "pi.oz", "pi.ap", "pi.ay", "pi.ar", "pe.ox", "pe.oy", "pe.oz", "snd.o",
        "te.o", "stl.val", "pi.fx", "pi.fy", "pi.fz", "pe.fx", "pe.fy", "pe.fz", "uc.a",
    ];
    const BLOB: [&str; 9] = [
        "raw", "tail", "set", "text", "ping", "nails", "hid.misc", "snd.f", "te.f",
    ];
    if id < N_LAZY {
        LAZY[id].into()
    } else if is_num(id) {
        NUM[id - NUM_BASE].into()
    } else if (RAW..=TEF).contains(&id) {
        BLOB[id - RAW].into()
    } else if (GEN_BASE..N_STREAMS).contains(&id) {
        format!("gen.{}", id - GEN_BASE)
    } else {
        format!("?{id}")
    }
}
