//! Windows 10+ native launch: job assignment happens inside CreateProcessW,
//! before any child thread runs. No shell and no spawn-then-assign window.
use std::{
    cmp::Ordering,
    ffi::{OsStr, OsString},
    fs::File,
    io,
    mem::{size_of, size_of_val},
    os::windows::{
        ffi::OsStrExt,
        io::{AsRawHandle, FromRawHandle, OwnedHandle},
    },
    process::Command,
    ptr::{null, null_mut},
};
use windows_sys::Win32::{
    Foundation::{
        DuplicateHandle, SetHandleInformation, DUPLICATE_SAME_ACCESS, ERROR_INSUFFICIENT_BUFFER,
        HANDLE, HANDLE_FLAG_INHERIT, WAIT_OBJECT_0, WAIT_TIMEOUT,
    },
    Globalization::{CompareStringOrdinal, CSTR_EQUAL, CSTR_GREATER_THAN, CSTR_LESS_THAN},
    System::{
        JobObjects::{
            CreateJobObjectW, JobObjectExtendedLimitInformation, SetInformationJobObject,
            TerminateJobObject, JOBOBJECT_EXTENDED_LIMIT_INFORMATION,
            JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
        },
        Pipes::CreatePipe,
        Threading::{
            CreateProcessW, DeleteProcThreadAttributeList, GetCurrentProcess,
            InitializeProcThreadAttributeList, UpdateProcThreadAttribute, WaitForSingleObject,
            CREATE_NO_WINDOW, CREATE_UNICODE_ENVIRONMENT, EXTENDED_STARTUPINFO_PRESENT,
            LPPROC_THREAD_ATTRIBUTE_LIST, PROCESS_INFORMATION, PROC_THREAD_ATTRIBUTE_HANDLE_LIST,
            PROC_THREAD_ATTRIBUTE_JOB_LIST, STARTF_USESTDHANDLES, STARTUPINFOEXW,
        },
    },
};

// OwnedHandle is Send/Sync and closes exactly once; no raw-handle ownership crosses threads.
pub struct ChildProcess {
    process: OwnedHandle,
    job: Option<OwnedHandle>,
    stdin: Option<OwnedHandle>,
    pid: u32,
}
impl ChildProcess {
    // Consumers pass absolute resource paths and use env/env_remove, never env_clear.
    pub fn spawn(command: &Command, log: File) -> io::Result<Self> {
        let program = wide_z(command.get_program())?;
        if program.len() == 1 || program.contains(&(b'"' as u16)) {
            return Err(invalid("invalid executable path"));
        }
        let mut command_line = quote_arg(&program[..program.len() - 1]);
        for arg in command.get_args() {
            let arg = wide_z(arg)?;
            command_line.push(b' ' as u16);
            command_line.extend(quote_arg(&arg[..arg.len() - 1]));
        }
        command_line.push(0);
        if command_line.len() > 32767 {
            return Err(invalid("command line exceeds Windows limit"));
        }
        let directory = command
            .get_current_dir()
            .map(|p| wide_z(p.as_os_str()))
            .transpose()?;
        let environment = environment_block(std::env::vars_os(), command)?;
        unsafe {
            let raw_job = CreateJobObjectW(null(), null());
            if raw_job.is_null() {
                return Err(io::Error::last_os_error());
            }
            let job = OwnedHandle::from_raw_handle(raw_job);
            let mut limits = JOBOBJECT_EXTENDED_LIMIT_INFORMATION::default();
            limits.BasicLimitInformation.LimitFlags = JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE;
            if SetInformationJobObject(
                raw_job,
                JobObjectExtendedLimitInformation,
                (&limits as *const JOBOBJECT_EXTENDED_LIMIT_INFORMATION).cast(),
                size_of_val(&limits) as u32,
            ) == 0
            {
                return Err(io::Error::last_os_error());
            }
            // NULL security attributes create non-inheritable pipe ends. Only the
            // read end is subsequently marked inheritable; the writer never is.
            let (mut read, mut write) = (null_mut(), null_mut());
            if CreatePipe(&mut read, &mut write, null(), 0) == 0 {
                return Err(io::Error::last_os_error());
            }
            let stdin_read = OwnedHandle::from_raw_handle(read);
            let stdin_write = OwnedHandle::from_raw_handle(write);
            if SetHandleInformation(read, HANDLE_FLAG_INHERIT, HANDLE_FLAG_INHERIT) == 0 {
                return Err(io::Error::last_os_error());
            }
            let mut raw_log = null_mut();
            let current = GetCurrentProcess();
            if DuplicateHandle(
                current,
                log.as_raw_handle(),
                current,
                &mut raw_log,
                0,
                1,
                DUPLICATE_SAME_ACCESS,
            ) == 0
            {
                return Err(io::Error::last_os_error());
            }
            let inherited_log = OwnedHandle::from_raw_handle(raw_log);
            let jobs = [raw_job];
            let inherited = [stdin_read.as_raw_handle(), inherited_log.as_raw_handle()];
            // Values must outlive the attribute list, including on every error path.
            let mut attributes = Attributes::new()?;
            attributes.set(PROC_THREAD_ATTRIBUTE_JOB_LIST, &jobs)?;
            attributes.set(PROC_THREAD_ATTRIBUTE_HANDLE_LIST, &inherited)?;
            let mut startup = STARTUPINFOEXW::default();
            startup.StartupInfo.cb = size_of::<STARTUPINFOEXW>() as u32;
            startup.StartupInfo.dwFlags = STARTF_USESTDHANDLES;
            startup.StartupInfo.hStdInput = read;
            startup.StartupInfo.hStdOutput = raw_log;
            startup.StartupInfo.hStdError = raw_log;
            startup.lpAttributeList = attributes.ptr();
            let mut process = PROCESS_INFORMATION::default();
            if CreateProcessW(
                program.as_ptr(),
                command_line.as_mut_ptr(),
                null(),
                null(),
                1,
                CREATE_NO_WINDOW | CREATE_UNICODE_ENVIRONMENT | EXTENDED_STARTUPINFO_PRESENT,
                environment.as_ptr().cast(),
                directory.as_ref().map_or(null(), |s| s.as_ptr()),
                &startup.StartupInfo,
                &mut process,
            ) == 0
            {
                return Err(io::Error::last_os_error());
            }
            // No fallible operation follows successful process creation.
            let process_handle = OwnedHandle::from_raw_handle(process.hProcess);
            let _thread = OwnedHandle::from_raw_handle(process.hThread);
            Ok(Self {
                process: process_handle,
                job: Some(job),
                stdin: Some(stdin_write),
                pid: process.dwProcessId,
            })
        }
    }
    pub fn id(&self) -> u32 {
        self.pid
    }
    pub fn running(&mut self) -> io::Result<bool> {
        match unsafe { WaitForSingleObject(self.process.as_raw_handle(), 0) } {
            WAIT_OBJECT_0 => Ok(false),
            WAIT_TIMEOUT => Ok(true),
            _ => Err(io::Error::last_os_error()),
        }
    }
    pub fn close_stdin(&mut self) {
        self.stdin.take();
    }
    pub fn kill_wait(&mut self) {
        self.close_stdin();
        if let Some(job) = self.job.take() {
            unsafe {
                TerminateJobObject(job.as_raw_handle(), 1);
            }
            // Closing the last non-inherited job handle enforces termination even
            // if TerminateJobObject failed. It also collects surviving descendants.
            drop(job);
            // Disk/driver stalls must not hang the tray thread indefinitely.
            match unsafe { WaitForSingleObject(self.process.as_raw_handle(), 5000) } {
                WAIT_OBJECT_0 => {}
                WAIT_TIMEOUT => eprintln!("desktop_child_termination_wait_timeout"),
                _ => eprintln!("desktop_child_termination_wait_failed"),
            }
        }
    }
}
impl Drop for ChildProcess {
    fn drop(&mut self) {
        self.kill_wait();
    }
}

struct Attributes {
    storage: Box<[usize]>,
}
impl Attributes {
    fn new() -> io::Result<Self> {
        let mut size = 0;
        unsafe {
            InitializeProcThreadAttributeList(null_mut(), 2, 0, &mut size);
            let err = io::Error::last_os_error();
            if err.raw_os_error() != Some(ERROR_INSUFFICIENT_BUFFER as i32) || size == 0 {
                return Err(err);
            }
            // Pointer-sized storage provides the native attribute-list alignment.
            let mut storage = vec![0usize; size.div_ceil(size_of::<usize>())].into_boxed_slice();
            if InitializeProcThreadAttributeList(storage.as_mut_ptr().cast(), 2, 0, &mut size) == 0
            {
                return Err(io::Error::last_os_error());
            }
            Ok(Self { storage })
        }
    }
    fn ptr(&mut self) -> LPPROC_THREAD_ATTRIBUTE_LIST {
        self.storage.as_mut_ptr().cast()
    }
    // The backing arrays are owned by spawn and remain live through CreateProcess.
    fn set(&mut self, attribute: u32, handles: &[HANDLE]) -> io::Result<()> {
        if unsafe {
            UpdateProcThreadAttribute(
                self.ptr(),
                0,
                attribute as usize,
                handles.as_ptr().cast(),
                size_of_val(handles),
                null_mut(),
                null(),
            )
        } == 0
        {
            return Err(io::Error::last_os_error());
        }
        Ok(())
    }
}
impl Drop for Attributes {
    fn drop(&mut self) {
        unsafe {
            DeleteProcThreadAttributeList(self.ptr());
        }
    }
}

fn invalid(message: &str) -> io::Error {
    io::Error::new(io::ErrorKind::InvalidInput, message)
}
fn wide_z(value: &OsStr) -> io::Result<Vec<u16>> {
    let mut wide: Vec<u16> = value.encode_wide().collect();
    if wide.contains(&0) {
        return Err(invalid("embedded NUL in process input"));
    }
    wide.push(0);
    Ok(wide)
}
// Microsoft CRT argument rules: backslashes are special only before a quote or
// the closing delimiter. Operate on UTF-16 units without lossy conversion.
fn quote_arg(arg: &[u16]) -> Vec<u16> {
    let mut out = vec![b'"' as u16];
    let mut slashes = 0;
    for &unit in arg {
        if unit == b'\\' as u16 {
            slashes += 1;
            continue;
        }
        out.extend(std::iter::repeat_n(
            b'\\' as u16,
            if unit == b'"' as u16 {
                slashes * 2 + 1
            } else {
                slashes
            },
        ));
        slashes = 0;
        out.push(unit);
    }
    out.extend(std::iter::repeat_n(b'\\' as u16, slashes * 2));
    out.push(b'"' as u16);
    out
}
fn compare_keys(a: &[u16], b: &[u16]) -> io::Result<Ordering> {
    match unsafe { CompareStringOrdinal(a.as_ptr(), a.len() as i32, b.as_ptr(), b.len() as i32, 1) }
    {
        CSTR_LESS_THAN => Ok(Ordering::Less),
        CSTR_EQUAL => Ok(Ordering::Equal),
        CSTR_GREATER_THAN => Ok(Ordering::Greater),
        _ => Err(io::Error::last_os_error()),
    }
}
fn environment_block(
    base: impl IntoIterator<Item = (OsString, OsString)>,
    command: &Command,
) -> io::Result<Vec<u16>> {
    let changes = base.into_iter().map(|(k, v)| (k, Some(v))).chain(
        command
            .get_envs()
            .map(|(k, v)| (k.to_owned(), v.map(OsStr::to_owned))),
    );
    let mut entries: Vec<(Vec<u16>, Vec<u16>)> = Vec::new();
    // ponytail: environment sizes are small; sorted insertion avoids locale/lossy
    // key canonicalization and preserves Win32 case-insensitive comparison.
    for (key, value) in changes {
        let mut key = wide_z(&key)?;
        key.pop();
        let drive_key = key.len() == 3
            && key[0] == b'=' as u16
            && key[2] == b':' as u16
            && (key[1] as u8).is_ascii_alphabetic()
            && key[1] <= 127;
        if key.is_empty() || key.len() > 32767 || (key.contains(&(b'=' as u16)) && !drive_key) {
            return Err(invalid("invalid environment variable name"));
        }
        let value = value.map(|v| wide_z(&v)).transpose()?;
        let mut at = entries.len();
        for (i, (existing, _)) in entries.iter().enumerate() {
            let ordering = compare_keys(existing, &key)?;
            if ordering != Ordering::Less {
                at = i;
                break;
            }
        }
        if at < entries.len() && compare_keys(&entries[at].0, &key)? == Ordering::Equal {
            entries.remove(at);
        }
        if let Some(value) = value {
            entries.insert(at, (key, value));
        }
    }
    let mut block = Vec::new();
    for (key, value) in entries {
        block.extend(key);
        block.push(b'=' as u16);
        block.extend(value); // Includes each entry's terminating NUL.
    }
    if block.is_empty() {
        block.push(0);
    }
    block.push(0);
    Ok(block)
}

#[cfg(test)]
mod tests {
    use super::*;
    use std::ffi::OsString;
    fn wide(value: &str) -> Vec<u16> {
        value.encode_utf16().collect()
    }

    #[test]
    fn quote_preserves_empty_spaces_quotes_and_trailing_backslashes() {
        for (input, expected) in [
            ("", "\"\""),
            ("a b", "\"a b\""),
            ("中文", "\"中文\""),
            ("a\"b", "\"a\\\"b\""),
            ("C:\\dir\\", "\"C:\\dir\\\\\""),
            ("a\\\"b", "\"a\\\\\\\"b\""),
        ] {
            assert_eq!(quote_arg(&wide(input)), wide(expected), "{input}");
        }
    }

    #[test]
    fn environment_overrides_and_removes_case_insensitively() {
        let base = [("Path", "old"), ("REMOVE_ME", "secret"), ("中文", "值")]
            .into_iter()
            .map(|(k, v)| (OsString::from(k), OsString::from(v)));
        let mut command = std::process::Command::new("unused.exe");
        command
            .env("PATH", "new")
            .env_remove("remove_me")
            .env("ADDED", "yes");
        let result = environment_block(base, &command).unwrap();
        assert_eq!(result, wide("ADDED=yes\0PATH=new\0中文=值\0\0"));
    }

    #[test]
    fn environment_preserves_drive_keys_and_unicode_case_overrides() {
        let base = [("=C:", "C:\\工作"), ("ÄREA", "old")]
            .into_iter()
            .map(|(k, v)| (OsString::from(k), OsString::from(v)));
        let mut command = std::process::Command::new("unused.exe");
        command.env("ärea", "new");
        assert_eq!(
            environment_block(base, &command).unwrap(),
            wide("=C:=C:\\工作\0ärea=new\0\0")
        );
    }

    #[test]
    fn environment_rejects_embedded_nul() {
        let command = std::process::Command::new("unused.exe");
        assert!(environment_block(
            [(OsString::from("A"), OsString::from("bad\0value"))],
            &command
        )
        .is_err());
    }
}
