'use strict';

function getCookie(key) {
	// extracted from https://developer.mozilla.org/en-US/docs/Web/API/document.cookie
	if (!key) return null;
	return decodeURIComponent(document.cookie.replace(new RegExp("(?:(?:^|.*;)\\s*" + encodeURIComponent(key).replace(/[\-\.\+\*]/g, "\\$&") + "\\s*\\=\\s*([^;]*).*$)|^.*$"), "$1")) || null;
}

function setCookie (key, value, maxAge, path, domain, secure) {
	// extracted from https://developer.mozilla.org/en-US/docs/Web/API/document.cookie
	document.cookie = encodeURIComponent(key) + "=" + encodeURIComponent(value) + (maxAge ? "; max-age=" + maxAge : "") + (domain ? "; domain=" + domain : "") + (path ? "; path=" + path : "") + (secure ? "; secure" : "");
}

// https://jsbin.com/qizepev/edit?html,css,js,output
function enableAutoExpand() {

	let textareas = document.querySelectorAll('.autoexpand');
	for (let i=0; i<textareas.length; i++) {
		textareas[i].classList.add('autoexpand-enabled');

		let hiddenDiv = document.createElement('div');
		hiddenDiv.classList.add('autoexpand-hidden');
		textareas[i].parentNode.insertBefore(hiddenDiv, textareas[i]);

		let cs = getComputedStyle(textareas[i]);
		let textareaMargin = parseFloat(cs.borderLeftWidth) + parseFloat(cs.paddingLeft) + parseFloat(cs.paddingRight) + parseFloat(cs.borderRightWidth);

		textareas[i].addEventListener('input', function () {
			let content = this.value;
			hiddenDiv.style.width = (this.getBoundingClientRect().width - textareaMargin).toFixed(3) + 'px';
			hiddenDiv.textContent = content + '\n\n';
			this.style.height = hiddenDiv.getBoundingClientRect().height + 'px';
		});
		textareas[i].dispatchEvent(new Event('input'));
	}
}

function onLoad() {
	var forms = document.querySelectorAll('form[method=POST]');
	if (forms.length == 0) {
		return;
	}

	var csrftoken = getCookie('csrftoken');
	if (!csrftoken) {
		// http://stackoverflow.com/a/12502559/559350
		// generate 32 pseudo-random characters
		csrftoken = '';
		for (var i=0; i<4; i++) {
			csrftoken += Math.random().toString(36).slice(2, 10);
		}
	}

	setCookie('csrftoken', csrftoken, 31449600, '/');

	for (var i=0; i<forms.length; i++) {
		var form = forms[i];
		var input = document.createElement('input');
		input.type = 'hidden';
		input.name = 'csrftoken';
		input.value = csrftoken;
		form.appendChild(input);
	}

	enableAutoExpand();
}

if (document.readyState == 'interactive' || document.readyState == 'complete') {
	onLoad();
} else {
	document.addEventListener('DOMContentLoaded', onLoad());
}
