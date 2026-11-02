#![no_main]
//! The attachment directory parser, and listing, reading and updating
//! attachments of arbitrary files.
use libfuzzer_sys::fuzz_target;

fuzz_target!(|data: &[u8]| {
    let _ = mvdz::format::parse_adir(data);
    let l = mvdz::Limits {
        memory: 256 << 20,
        ..Default::default()
    };
    if let Ok(list) = mvdz::list_attachments(std::io::Cursor::new(data), "") {
        for a in list.iter().take(4) {
            let _ = mvdz::read_attachment_with(std::io::Cursor::new(data), &a.name, &l);
        }
        let add = [mvdz::Attachment {
            name: "x".into(),
            data: data[..data.len().min(64)].to_vec(),
            ..Default::default()
        }];
        // an export keeping every other attachment is accepted by readers
        let mut out = Vec::new();
        let drop: Vec<String> = list.iter().step_by(2).map(|a| a.name.clone()).collect();
        let sel = mvdz::Selection {
            drop,
            ..Default::default()
        };
        if mvdz::export_attachments(std::io::Cursor::new(data), &mut out, |e| sel.matches(e))
            .is_ok()
        {
            assert_eq!(
                mvdz::read_metadata(std::io::Cursor::new(&out))
                    .unwrap()
                    .attachments
                    .len(),
                list.len() / 2
            );
        }
        if let Ok(b) = mvdz::update_attachments_bytes(data, &["y"], &add) {
            assert_eq!(
                mvdz::read_attachment(std::io::Cursor::new(&b), "x").unwrap(),
                add[0].data
            );
        }
    }
});
