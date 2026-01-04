import mermaid from 'https://cdn.jsdelivr.net/npm/mermaid@11/dist/mermaid.esm.min.mjs';

function getThemeConfig() {
  const isDark = document.documentElement.getAttribute('data-theme') === 'dark';
  const textColor = isDark ? '#e2e8f0' : '#1e293b';

  return {
    startOnLoad: false,
    look: 'handDrawn',
    theme: 'base',
    themeVariables: {
      // No fills - transparent backgrounds
      primaryColor: 'transparent',
      secondaryColor: 'transparent',
      tertiaryColor: 'transparent',
      // Blue borders matching Galaxy theme
      primaryBorderColor: '#3b82f6',
      secondaryBorderColor: '#3b82f6',
      tertiaryBorderColor: '#3b82f6',
      // Text colors - match current mode
      primaryTextColor: textColor,
      secondaryTextColor: textColor,
      tertiaryTextColor: textColor,
      // Lines and edges in blue
      lineColor: '#3b82f6',
      // Edge labels - blue text, no background
      edgeLabelBackground: 'transparent',
      labelBackground: 'transparent',
      labelTextColor: '#3b82f6',
      // Background
      background: 'transparent',
      mainBkg: 'transparent',
      nodeBkg: 'transparent',
      // Git graph specific
      git0: '#3b82f6',
      git1: '#3b82f6',
      gitBranchLabel0: '#3b82f6',
      gitBranchLabel1: '#3b82f6',
      commitLabelBackground: 'transparent',
      // Fonts
      fontFamily: 'system-ui, -apple-system, sans-serif',
    },
  };
}

mermaid.initialize(getThemeConfig());

async function renderMermaid() {
  // Starlight uses expressive-code which wraps code in a complex structure
  // Find all pre elements with data-language="mermaid" that haven't been processed yet
  const mermaidBlocks = document.querySelectorAll('pre[data-language="mermaid"]:not([data-mermaid-processed])');

  for (const pre of mermaidBlocks) {
    // Mark as processed immediately to prevent duplicate processing
    pre.setAttribute('data-mermaid-processed', 'true');
    // Extract text content from all the span elements inside
    // The structure is: pre > code > div.ec-line > div.code > span
    const lines = pre.querySelectorAll('.ec-line');
    let content = '';

    if (lines.length > 0) {
      // Expressive-code structure
      lines.forEach(line => {
        const codeDiv = line.querySelector('.code');
        if (codeDiv) {
          content += codeDiv.textContent + '\n';
        }
      });
    } else {
      // Fallback to simple structure
      const code = pre.querySelector('code');
      content = code ? code.textContent : pre.textContent;
    }

    content = content.trim();
    if (!content) continue;

    // Create mermaid container
    const div = document.createElement('div');
    div.className = 'mermaid';
    div.textContent = content;

    // Find the outermost wrapper
    const figure = pre.closest('figure.frame');
    const wrapper = figure ? figure.closest('.expressive-code') : null;
    const targetElement = wrapper || figure || pre;

    // Create a wrapper div to maintain proper block-level spacing
    const containerDiv = document.createElement('div');
    containerDiv.className = 'mermaid-container';
    containerDiv.style.cssText = 'display: block; margin: 1rem 0;';
    containerDiv.appendChild(div);

    // Hide the original element instead of removing it to preserve DOM/CSS structure
    // This prevents breaking CSS selectors that depend on sibling relationships
    targetElement.style.display = 'none';
    targetElement.setAttribute('data-mermaid-hidden', 'true');

    // Insert the mermaid diagram after the hidden element
    targetElement.parentNode.insertBefore(containerDiv, targetElement.nextSibling);
  }

  // Run mermaid if we found any diagrams
  const diagrams = document.querySelectorAll('.mermaid');
  if (diagrams.length > 0) {
    try {
      await mermaid.run();
    } catch (e) {
      console.error('Mermaid rendering error:', e);
    }
  }
}

// Run on initial load
if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', renderMermaid);
} else {
  renderMermaid();
}

// Re-run on Astro view transitions
document.addEventListener('astro:page-load', renderMermaid);

// Re-render when theme changes
const observer = new MutationObserver((mutations) => {
  for (const mutation of mutations) {
    if (mutation.attributeName === 'data-theme') {
      mermaid.initialize(getThemeConfig());
      renderMermaid();
    }
  }
});
observer.observe(document.documentElement, { attributes: true });
