//! A minimal SIGPROF sampling profiler for the command-line tool, built
//! only with the `sampler` feature on x86_64 Linux (perf is unavailable on the build
//! machine). `MVDZ_SAMPLE=out.txt` records the interrupted instruction
//! pointer of every thread at ~2 kHz of CPU time; addresses are written
//! relative to the executable's load base, for `addr2line -f -i -C -e`.

use std::sync::atomic::{AtomicUsize, Ordering};

const CAP: usize = 1 << 22;
static N: AtomicUsize = AtomicUsize::new(0);
static mut BUF: [usize; CAP] = [0; CAP];

extern "C" fn handler(_sig: libc::c_int, _info: *mut libc::siginfo_t, ctx: *mut libc::c_void) {
    unsafe {
        let uc = ctx as *const libc::ucontext_t;
        let ip = (*uc).uc_mcontext.gregs[libc::REG_RIP as usize] as usize;
        let i = N.fetch_add(1, Ordering::Relaxed);
        if i < CAP {
            *std::ptr::addr_of_mut!(BUF[i]) = ip;
        }
    }
}

pub fn start() {
    unsafe {
        let mut sa: libc::sigaction = std::mem::zeroed();
        sa.sa_sigaction = handler as *const () as usize;
        sa.sa_flags = libc::SA_SIGINFO | libc::SA_RESTART;
        libc::sigaction(libc::SIGPROF, &sa, std::ptr::null_mut());
        let tv = libc::timeval {
            tv_sec: 0,
            tv_usec: 500,
        };
        let it = libc::itimerval {
            it_interval: tv,
            it_value: tv,
        };
        libc::setitimer(libc::ITIMER_PROF, &it, std::ptr::null_mut());
    }
}

pub fn stop(path: &str) {
    unsafe {
        let z = libc::timeval {
            tv_sec: 0,
            tv_usec: 0,
        };
        let it = libc::itimerval {
            it_interval: z,
            it_value: z,
        };
        libc::setitimer(libc::ITIMER_PROF, &it, std::ptr::null_mut());
    }
    let maps = std::fs::read_to_string("/proc/self/maps").unwrap_or_default();
    let exe = std::fs::read_link("/proc/self/exe").unwrap_or_default();
    let exe_s = exe.to_string_lossy().to_string();
    let mut base = 0usize;
    for l in maps.lines() {
        if l.ends_with(&exe_s) {
            let a = l.split('-').next().unwrap_or("0");
            let off = l.split_whitespace().nth(2).unwrap_or("0");
            if usize::from_str_radix(off, 16).unwrap_or(1) == 0 {
                base = usize::from_str_radix(a, 16).unwrap_or(0);
                break;
            }
        }
    }
    let n = N.load(Ordering::Relaxed).min(CAP);
    let mut s = String::new();
    for i in 0..n {
        let ip = unsafe { *std::ptr::addr_of!(BUF[i]) };
        s += &format!("{:x}\n", ip.wrapping_sub(base));
    }
    std::fs::write(path, s).ok();
    eprintln!("sampler: {n} samples -> {path} (exe {exe_s})");
}

/// Samples while alive; writes the samples to its path when dropped.
pub struct Guard(String);

impl Guard {
    pub fn start(path: String) -> Guard {
        start();
        Guard(path)
    }
}

impl Drop for Guard {
    fn drop(&mut self) {
        stop(&self.0);
    }
}
