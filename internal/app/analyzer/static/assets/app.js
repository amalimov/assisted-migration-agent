const api = "/api/v1";
const message = document.querySelector("#message");
const collectorStatus = document.querySelector("#collector-status");
const collectionsList = document.querySelector("#collections");
const vmTable = document.querySelector("#virtual-machines");
const vmSummary = document.querySelector("#vm-summary");
const exportLink = document.querySelector("#export-link");
let selectedCollection = "";
let previousStatus = "";

async function request(path, options = {}) {
  const response = await fetch(`${api}${path}`, options);
  if (!response.ok) {
    const body = await response.json().catch(() => ({}));
    throw new Error(body.error || `Request failed (${response.status})`);
  }
  return response.status === 204 ? null : response.json();
}

function showMessage(text, isError = false) {
  message.textContent = text;
  message.classList.toggle("error", isError);
}

async function refreshStatus() {
  try {
    const result = await request("/collector");
    collectorStatus.textContent = result.status;
    if (result.error) showMessage(result.error, true);
    if (result.status === "collected" && previousStatus !== "collected") await refreshCollections();
    previousStatus = result.status;
  } catch (error) {
    collectorStatus.textContent = "Unavailable";
    showMessage(error.message, true);
  }
}

async function refreshCollections() {
  try {
    const result = await request("/collections");
    collectionsList.replaceChildren();
    const collections = result.collections || [];
    if (!collections.length) {
      const empty = document.createElement("li");
      empty.textContent = "No collections yet.";
      collectionsList.append(empty);
      return;
    }
    for (const collection of collections) {
      const item = document.createElement("li");
      const button = document.createElement("button");
      button.type = "button";
      button.textContent = `${collection.id} · ${new Date(collection.createdAt).toLocaleString()}`;
      button.setAttribute("aria-current", String(collection.id === selectedCollection));
      button.addEventListener("click", () => selectCollection(collection.id));
      item.append(button);
      collectionsList.append(item);
    }
  } catch (error) {
    showMessage(error.message, true);
  }
}

async function selectCollection(id) {
  selectedCollection = id;
  exportLink.href = `${api}/collections/${encodeURIComponent(id)}/export?format=zip`;
  exportLink.hidden = false;
  await refreshCollections();
  try {
    const result = await request(`/collections/${encodeURIComponent(id)}/virtualmachines?page=1&pageSize=100`);
    vmTable.replaceChildren();
    vmSummary.textContent = `${result.total || 0} virtual machines. Showing the first 100.`;
    for (const vm of result.virtualMachines || []) {
      const row = document.createElement("tr");
      for (const value of [vm.name, vm.cluster, vm.memory, vm.issueCount]) {
        const cell = document.createElement("td");
        cell.textContent = value ?? "—";
        row.append(cell);
      }
      vmTable.append(row);
    }
  } catch (error) {
    showMessage(error.message, true);
  }
}

document.querySelector("#upload-form").addEventListener("submit", async (event) => {
  event.preventDefault();
  const button = event.currentTarget.querySelector("button");
  button.disabled = true;
  try {
    const files = document.querySelector("#files").files;
    const body = new FormData();
    for (const file of files) body.append("files", file);
    await request("/collector/rvtools", { method: "POST", body });
    showMessage("Analysis started. Collection status will update automatically.");
    await refreshStatus();
  } catch (error) {
    showMessage(error.message, true);
  } finally {
    button.disabled = false;
  }
});

document.querySelector("#refresh").addEventListener("click", refreshCollections);
refreshStatus();
refreshCollections();
setInterval(refreshStatus, 2500);
