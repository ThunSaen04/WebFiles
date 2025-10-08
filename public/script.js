const uploadBtn = document.getElementById("uploadBtn");
const fileInput = document.getElementById("fileInput");
const fileTable = document.querySelector("#fileTable tbody");

// โหลดไฟล์
async function loadFiles() {
  const res = await fetch("/files");
  const files = await res.json();
  fileTable.innerHTML = "";

  files.forEach(f => {
    const row = document.createElement("tr");

        row.innerHTML = `
        <td>${getFileIcon(f.filename)} ${f.filename}</td>
        <td>${formatFileSize(f.size)}</td>
        <td>
            <a href="/download/${encodeURIComponent(f.filename)}" target="_blank" class="btn btn-success btn-sm me-1">
            <i class="bi bi-download"></i> ดาวน์โหลด
            </a>
            <button class="btn btn-danger btn-sm" onclick="deleteFile('${f.filename}')">
            <i class="bi bi-trash"></i> ลบ
            </button>
        </td>
        `;

    fileTable.appendChild(row);
  });
}

function getFileIcon(filename) {
  const ext = filename.split('.').pop().toLowerCase();

  switch(ext) {
    // เอกสาร
    case 'pdf': return '<i class="bi bi-file-earmark-pdf-fill text-danger"></i>';
    case 'doc':
    case 'docx': return '<i class="bi bi-file-earmark-word-fill text-primary"></i>';
    case 'xls':
    case 'xlsx': return '<i class="bi bi-file-earmark-excel-fill text-success"></i>';
    case 'ppt':
    case 'pptx': return '<i class="bi bi-file-earmark-ppt-fill text-warning"></i>';
    case 'txt': return '<i class="bi bi-file-earmark-text-fill text-muted"></i>';

    // รูปภาพ
    case 'jpg':
    case 'jpeg':
    case 'png':
    case 'gif': return '<i class="bi bi-file-earmark-image-fill text-info"></i>';

    // วิดีโอ
    case 'mp4':
    case 'mov':
    case 'mkv':
    case 'webm': return '<i class="bi bi-file-earmark-play-fill text-warning"></i>';

    // เสียง
    case 'mp3':
    case 'wav':
    case 'ogg': return '<i class="bi bi-file-earmark-music-fill text-secondary"></i>';

    // ไฟล์บีบอัด
    case 'zip':
    case 'rar':
    case '7z': return '<i class="bi bi-file-earmark-zip-fill text-dark"></i>';

    // ไฟล์อื่นๆ
    default: return '<i class="bi bi-file-earmark-fill"></i>';
  }
}

// แปลงขนาดไฟล์เป็น Dynamic (Bytes → KB / MB / GB)
function formatFileSize(bytes) {
  if (bytes < 1024) return bytes + ' B';
  const kb = bytes / 1024;
  if (kb < 1024) return kb.toFixed(2) + ' KB';
  const mb = kb / 1024;
  if (mb < 1024) return mb.toFixed(2) + ' MB';
  const gb = mb / 1024;
  return gb.toFixed(2) + ' GB';
}

// อัปโหลดไฟล์
function uploadFile() {
  const file = fileInput.files[0];
  if (!file) { Swal.fire({icon:'warning',title:'กรุณาเลือกไฟล์ก่อน'}); return; }

  const formData = new FormData();
  formData.append("file", file);

  const xhr = new XMLHttpRequest();
  const progressContainer = document.getElementById("uploadProgress");
  const progressBar = document.getElementById("uploadProgressBar");

  xhr.upload.addEventListener("progress", (e) => {
    if(e.lengthComputable){
      const percent = Math.round((e.loaded/e.total)*100);
      progressContainer.style.display = "block";
      progressBar.style.width = percent + "%";
      progressBar.textContent = percent + "%";
    }
  });

  xhr.onload = function(){
    progressContainer.style.display = "none";
    progressBar.style.width = "0%";
    progressBar.textContent = "0%";
    if(xhr.status===200){ Swal.fire({icon:'success',title:'อัปโหลดสำเร็จ!',timer:1500,showConfirmButton:false}); fileInput.value=""; loadFiles(); }
    else{ Swal.fire({icon:'error',title:'เกิดข้อผิดพลาด',text:xhr.responseText}); }
  }

  xhr.open("POST","/upload");
  xhr.send(formData);
}



// ลบไฟล์
async function deleteFile(name) {
  const result = await Swal.fire({
    title: `ต้องการลบไฟล์ ${name} ใช่หรือไม่?`,
    icon: 'warning',
    showCancelButton: true,
    confirmButtonColor: '#dc3545',
    cancelButtonColor: '#3085d6',
    confirmButtonText: 'ลบ',
    cancelButtonText: 'ยกเลิก'
  });

  if (result.isConfirmed) {
    const res = await fetch(`/delete/${encodeURIComponent(name)}`, { method: "DELETE" });
    const data = await res.json();
    if (res.ok) {
      Swal.fire({
        icon: 'success',
        title: `ลบไฟล์ ${name} สำเร็จ`,
        timer: 1500,
        showConfirmButton: false
      });
      loadFiles();
    } else {
      Swal.fire({
        icon: 'error',
        title: 'ไม่สามารถลบไฟล์ได้',
        text: data.error
      });
    }
  }
}

// Event listeners
uploadBtn.addEventListener("click", uploadFile);
window.addEventListener("load", loadFiles);
